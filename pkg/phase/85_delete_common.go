// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package phase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/pkg/retry"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	deleteNode = `delete node %s`
)

// DeleteCommon phase state
type DeleteCommon struct {
	GenericPhase
	Distro      distrocfg.Distro
	TargetHosts []string
	leader      *cluster.Host
	// nodeNames maps a host to the name its node carries in the cluster. See resolveNodeNames.
	nodeNames map[string]string
	// keepEngineForDelete is set when the engine has to stay up on a controller while its node is
	// deleted, which is the case only when the removal leaves a single controller behind. See
	// stopEngineBeforeDelete.
	keepEngineForDelete bool
}

// ErrNoSurvivingController is returned when every running controller is being removed, but some
// host is not. Tearing a subset down works by driving kubectl through a controller that stays, so
// a run with no controller left to drive is a whole-cluster teardown wearing a subset's clothes:
// it would drain and delete nodes through a machine it is about to uninstall, and whether that
// finished would depend on the order the hosts happened to be in.
//
// Removing every host is still allowed. That is a reset, and reset is what it is for.
var ErrNoSurvivingController = errors.New("every running controller is being removed, but not every host is: " +
	"target the whole cluster to tear it down, or leave a controller out of the target list to remove a subset through it")

// ErrNoRunningController is returned when a reset of part of a cluster finds no controller running
// the engine it was told to remove.
//
// The deletions are kubectl commands run on a controller that stays, so with none answering there
// is nothing to drive them with. Skipping them instead -- which is what used to happen -- leaves
// the targeted host with its files removed, its engine still running and its node still in the
// cluster, and the run reports success over it.
//
// The usual cause is a run pointed at the wrong engine: the controller service it looks for is the
// one `--distro` named, not the one the fleet runs, so a k3s cluster reset as rke2 looks exactly
// like a cluster whose control plane is down.
var ErrNoRunningController = errors.New("no controller is running the engine this reset was told to remove: " +
	"removing a subset of the hosts needs a controller that stays, to drain and delete their nodes through. " +
	"Check that --distro, or distro.type in the cargoship config file, names the engine the fleet actually " +
	"runs, or reset the whole cluster rather than a subset")

// ErrControllersNotOneAtATime is returned when a run that keeps the cluster removes more than one
// running controller.
//
// Each removal is a raft reconfiguration that needs a majority of the membership it starts from,
// and cargoship takes the engine down on a controller before deleting its node so the member
// cannot vote or re-register behind the run. Two removals in one run therefore pass through a
// state the second one cannot commit from: three members, one already stopped, is a majority of
// two out of three; the next stop leaves one of two, and the removal hangs with the cluster
// unavailable rather than failing cleanly.
//
// Removing them in separate runs is safe because the cluster is whole again at the start of each.
var ErrControllersNotOneAtATime = errors.New("more than one running controller is being removed in a single run: " +
	"remove them one run at a time, so each removal starts from a cluster that still has every member")

// Prepare the phase
func (p *DeleteCommon) Prepare(ctx context.Context, _ *cluster.Cluster, _ *distro.Distro) error {
	hosts := p.manager.Config.Spec.Hosts
	control := hosts.Filter(func(h *cluster.Host) bool {
		return h.ServiceIsRunning(ctx, p.Distro.GetControllerService()) && h.IsController()
	})
	if len(control) == 0 {
		// Removing a subset needs a controller that stays; a whole-cluster reset needs none, and
		// a dry run never started an engine to find. See ErrNoRunningController.
		if !p.manager.DryRun && len(filterTargetHosts(hosts, p.TargetHosts)) != len(hosts) {
			return fmt.Errorf("%w: nothing answers for the %s service", ErrNoRunningController, p.Distro.GetControllerService())
		}
		logger.From(ctx).Warn("there is no running controllers")
		return nil
	}

	// The leader has to be a controller that survives the run: every node deletion is a kubectl
	// command run on it, and the phases that follow uninstall the engine from everything in the
	// target list.
	surviving := control.Filter(func(h *cluster.Host) bool {
		return !matchesTargetHost(h, p.TargetHosts)
	})
	if len(surviving) > 0 {
		p.leader = surviving[0]
		return p.planControllerRemovals(ctx, control, surviving)
	}

	// No controller survives. That is correct for a whole-cluster reset and wrong for anything
	// else, so the two are told apart by whether anything is being kept.
	if len(filterTargetHosts(hosts, p.TargetHosts)) == len(hosts) {
		p.leader = control[0]
		return nil
	}
	return ErrNoSurvivingController
}

// planControllerRemovals checks what the run does to the controller membership, and decides
// whether the engine comes down before or after the node is deleted.
//
// control is every controller with a running engine; surviving is the subset the run keeps.
func (p *DeleteCommon) planControllerRemovals(ctx context.Context, control, surviving cluster.Hosts) error {
	removing := len(control) - len(surviving)
	if removing > 1 {
		return fmt.Errorf("%w: %d of %d running controllers are targeted", ErrControllersNotOneAtATime, removing, len(control))
	}

	// Taking the engine down first is what makes the node deletion stick: an engine that is still
	// running re-registers the node object seconds after it is deleted, and the etcd member it
	// owns is removed on deletion only once it has stopped. The exception is the removal that
	// leaves one controller, where the member being removed has to still be up to form the
	// majority of two that commits its own removal.
	p.keepEngineForDelete = removing > 0 && len(surviving) < 2
	if p.keepEngineForDelete {
		logger.From(ctx).Warn("this removal leaves a single controller, so the engine stays up until the node is deleted",
			"controllers", len(control))
	}
	return nil
}

// stopEngineBeforeDelete gives a controller's cluster membership up before its node is deleted, by
// whichever route its distro uses, unless the membership arithmetic in planControllerRemovals says
// the engine has to stay up.
func (p *DeleteCommon) stopEngineBeforeDelete(ctx context.Context, h *cluster.Host) error {
	// A distro that implements PreUninstallResetter gives its membership up by resetting itself,
	// not by having a controller remove the member when the node object goes away: on kubeadm
	// `kubectl delete node` touches no membership, and `kubeadm reset`'s remove-etcd-member phase
	// needs the local etcd -- a static pod kubelet owns -- still running to commit the removal, so
	// stopping the engine first is what strands it. The reset stops the kubelet itself, which is
	// why no stop follows; phase 88 runs it again over a host that is already reset, and absorbs
	// the error that second run reports.
	//
	// keepEngineForDelete is deliberately not consulted here. It exists for the delete-driven
	// removal, and the member has to be up for a reset whatever the arithmetic says.
	if r, ok := p.Distro.(distrocfg.PreUninstallResetter); ok {
		logger.From(ctx).Info("resetting the host before deleting its node", "host", h)
		return r.PreUninstallReset(ctx, h)
	}
	if p.keepEngineForDelete {
		return nil
	}
	logger.From(ctx).Info("stopping the engine before deleting the node", "host", h)
	if err := p.Distro.StopControllerService(h); err != nil {
		return fmt.Errorf("stop the engine on %s before deleting its node: %w", h, err)
	}
	return nil
}

// stopAgentBeforeDelete is the worker's half of the same ordering, and it is needed for the same
// reason: k3s and RKE2 drop a node when its Node object is deleted, but a kubelet that is still
// running re-registers it within seconds, so a deletion raced against a live agent leaves the
// node in the cluster. The engine is then uninstalled and the object is stranded NotReady with
// nothing left to remove it.
//
// No membership arithmetic here. keepEngineForDelete exists because a controller removal that
// leaves one controller needs the departing member up to commit its own removal; a worker is
// not an etcd member and its engine can always come down first.
func (p *DeleteCommon) stopAgentBeforeDelete(ctx context.Context, h *cluster.Host) error {
	logger.From(ctx).Info("stopping the engine before deleting the node", "host", h)
	if err := p.Distro.StopWorkerService(h); err != nil {
		return fmt.Errorf("stop the engine on %s before deleting its node: %w", h, err)
	}
	return nil
}

// matchesTargetHost reports whether h is one of the hosts named in targets.
//
// A host is matched by any name an operator could have written: the hostname in the configuration,
// the hostname the host reported once it was visited, and the address the configuration connects
// through. The address comes from the connection configuration rather than from Host.Address,
// which reads the live rig client and is empty for a host that has not been dialled -- so matching
// on it would depend on how far through a run the phase is, and would silently fall back to
// hostname-only in exactly the case a target list is being checked against hosts that are about to
// be removed.
func matchesTargetHost(h *cluster.Host, targets []string) bool {
	if len(targets) == 0 {
		return false
	}
	for _, t := range targets {
		if t == "" {
			continue
		}
		for _, name := range hostIdentifiers(h) {
			if strings.EqualFold(t, name) {
				return true
			}
		}
	}
	return false
}

// hostIdentifiers are every name a host answers to, for matching a target against it.
func hostIdentifiers(h *cluster.Host) []string {
	names := []string{h.Hostname, h.Metadata.Hostname}
	if cfg := h.ConnectionConfig.SSH; cfg != nil {
		names = append(names, cfg.Address)
	}
	if h.Configurer != nil {
		names = append(names, h.Configurer.Hostname(h))
	}

	out := make([]string, 0, len(names))
	for _, name := range names {
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// CheckTargetHosts reports a target that names no host in the configuration.
//
// A target nobody matches is the quiet failure this whole feature could have: the filter returns
// an empty set, every phase reports nothing to do, and the run succeeds having removed nothing. A
// typo in a hostname is the ordinary way to get there.
func CheckTargetHosts(hosts cluster.Hosts, targets []string) error {
	if len(targets) == 0 {
		return nil
	}

	known := make([]string, 0, len(hosts))
	for _, h := range hosts {
		known = append(known, hostIdentifiers(h)...)
	}

	var unmatched []string
	for _, t := range targets {
		if t == "" {
			continue
		}
		if !slices.ContainsFunc(known, func(name string) bool { return strings.EqualFold(t, name) }) {
			unmatched = append(unmatched, t)
		}
	}
	if len(unmatched) == 0 {
		return nil
	}

	sort.Strings(known)
	return fmt.Errorf("no host in the configuration is named %s: the hosts it holds are %s",
		strings.Join(unmatched, ", "), strings.Join(slices.Compact(known), ", "))
}

// filterTargetHosts returns hosts matching targets. If targets is empty, it returns all hosts.
func filterTargetHosts(hosts cluster.Hosts, targets []string) cluster.Hosts {
	if len(targets) == 0 {
		return hosts
	}
	return hosts.Filter(func(h *cluster.Host) bool {
		return matchesTargetHost(h, targets)
	})
}

func (p *DeleteCommon) drainNode(ctx context.Context, h *cluster.Host) error {
	logger.From(ctx).Info("draining", "node", h)
	return p.drainClusterNode(ctx, p.leader, p.Distro, nodeNameFor(p.nodeNames, h))
}

func (p *DeleteCommon) deleteNode(ctx context.Context, h *cluster.Host) error {
	logger.From(ctx).Info("deleting", "node", h)
	err := retry.Timeout(ctx, 10*time.Second, func(_ context.Context) error {
		return p.leader.Sudo().Exec(p.Distro.KubectlCmdf(p.leader, p.Distro.DataDirPath(), deleteNode, nodeNameFor(p.nodeNames, h)))
	})
	if err != nil {
		logger.From(ctx).Warn("got an error well deleting the", "node", h.Configurer.Hostname(h))
	}
	return nil
}
