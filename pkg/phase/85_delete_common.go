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

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/pkg/retry"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	deleteNode = `delete node %s`

	// listNodeIdentities prints one line per node: its name, then the addresses it reports as
	// one comma-separated field. That is what a host is matched against, because the name
	// alone is not enough -- see resolveNodeNames.
	//
	// custom-columns rather than jsonpath. The command is built as a shell string and run
	// through an SSH session, and a jsonpath template carrying the double quotes and backslash
	// of {"\n"} does not survive that: the shell strips the quotes and kubectl rejects what is
	// left, which showed up as every host failing to match any node. This form has nothing in
	// it a shell will touch.
	listNodeIdentities = `get nodes --no-headers ` +
		`-o custom-columns=:.metadata.name,:.status.addresses[*].address 2>/dev/null`
)

// DeleteCommon phase state
type DeleteCommon struct {
	GenericPhase
	Distro      distrocfg.Distro
	TargetHosts []string
	leader      *cluster.ZarfHost
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
func (p *DeleteCommon) Prepare(ctx context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	hosts := p.manager.Config.Spec.Hosts
	control := hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.ServiceIsRunning(ctx, p.Distro.GetControllerService()) && h.IsController()
	})
	if len(control) == 0 {
		logger.From(ctx).Warn("there is no running controllers")
		return nil
	}

	// The leader has to be a controller that survives the run: every node deletion is a kubectl
	// command run on it, and the phases that follow uninstall the engine from everything in the
	// target list.
	surviving := control.Filter(func(h *cluster.ZarfHost) bool {
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
func (p *DeleteCommon) planControllerRemovals(ctx context.Context, control, surviving cluster.ZarfHosts) error {
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

// stopEngineBeforeDelete takes the engine down on a host whose node is about to be deleted, unless
// the membership arithmetic in planControllerRemovals says it has to stay up.
func (p *DeleteCommon) stopEngineBeforeDelete(ctx context.Context, h *cluster.ZarfHost) error {
	if p.keepEngineForDelete {
		return nil
	}
	logger.From(ctx).Info("stopping the engine before deleting the node", "host", h)
	if err := p.Distro.StopControllerService(h); err != nil {
		return fmt.Errorf("stop the engine on %s before deleting its node: %w", h, err)
	}
	return nil
}

// resolveNodeNames maps each host to the name its node carries in the cluster, for the hosts
// that have one.
//
// A host cannot be matched to its node by one guessed name. The node's name comes from the
// engine config's `node-name`, which cargoship writes from the inventory's hostname, while
// `h.Configurer.Hostname(h)` is what the machine reports -- and on any host with a search
// domain those differ: the machine says `kw1.cargoship.test` and the node is `kw1`. Probing the
// reported name returned "not found" for a node that was there, the host was dropped from the
// phase, and the engine was uninstalled by a later phase that does no such probe. The node
// stayed in the cluster with nothing left to remove it.
//
// So the nodes are listed once and matched on any identifier the host and the node share,
// which is the same notion of "this node is this host" that DetectRemovedHosts already uses on
// the apply side. One kubectl call for the whole phase rather than one per host.
//
// A host with no matching node is absent from the result, which is how the phases tell apart
// "this host is in the cluster" from "this host is not", without a second probe.
func (p *DeleteCommon) resolveNodeNames(ctx context.Context, hosts cluster.ZarfHosts) map[string]string {
	if p.leader == nil || len(hosts) == 0 {
		return nil
	}

	out, err := p.leader.Sudo().ExecOutput(
		p.Distro.KubectlCmdf(p.leader, p.Distro.DataDirPath(), listNodeIdentities))
	if err != nil {
		logger.From(ctx).Warn("could not list the cluster's nodes, so no host can be matched to one", "error", err)
		return nil
	}
	return matchNodesToHosts(out, hosts)
}

// matchNodesToHosts pairs the lines listNodeIdentities printed with the hosts they belong to.
//
// Split out from resolveNodeNames so the matching is testable without a cluster: the parsing
// and the identifier comparison are where this goes wrong, not the kubectl call.
func matchNodesToHosts(listed string, hosts cluster.ZarfHosts) map[string]string {
	byHost := make(map[string]string, len(hosts))

	for line := range strings.SplitSeq(listed, "\n") {
		// Whitespace separates the name from the address column; commas separate the addresses
		// within it. Splitting on both leaves one identity per element whichever column it
		// came from, and the node's own name is the first.
		fields := strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t'
		})
		if len(fields) == 0 {
			continue
		}
		node := fields[0]

		identities := make(map[string]struct{}, len(fields))
		for _, f := range fields {
			identities[strings.ToLower(f)] = struct{}{}
		}

		for _, h := range hosts {
			if _, taken := byHost[h.String()]; taken {
				continue
			}
			if hostMatchesIdentities(h, identities) {
				byHost[h.String()] = node
			}
		}
	}
	return byHost
}

// hostMatchesIdentities reports whether any name or address the host is known by appears among
// the identities a node reported. The set is the same one DetectRemovedHosts builds.
func hostMatchesIdentities(h *cluster.ZarfHost, identities map[string]struct{}) bool {
	for _, id := range []string{h.Metadata.Hostname, h.Hostname, h.PrivateAddress, h.Address()} {
		if id == "" {
			continue
		}
		if _, ok := identities[strings.ToLower(id)]; ok {
			return true
		}
	}
	return false
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
func (p *DeleteCommon) stopAgentBeforeDelete(ctx context.Context, h *cluster.ZarfHost) error {
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
// through. The address comes from the connection configuration rather than from ZarfHost.Address,
// which reads the live rig client and is empty for a host that has not been dialled -- so matching
// on it would depend on how far through a run the phase is, and would silently fall back to
// hostname-only in exactly the case a target list is being checked against hosts that are about to
// be removed.
func matchesTargetHost(h *cluster.ZarfHost, targets []string) bool {
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
func hostIdentifiers(h *cluster.ZarfHost) []string {
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
func CheckTargetHosts(hosts cluster.ZarfHosts, targets []string) error {
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
func filterTargetHosts(hosts cluster.ZarfHosts, targets []string) cluster.ZarfHosts {
	if len(targets) == 0 {
		return hosts
	}
	return hosts.Filter(func(h *cluster.ZarfHost) bool {
		return matchesTargetHost(h, targets)
	})
}

func (p *DeleteCommon) drainNode(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("draining", "node", h)
	return p.manager.RetryTimeout(ctx, func(_ context.Context) error {
		return p.leader.Sudo().Exec(p.Distro.KubectlCmdf(p.leader, p.Distro.DataDirPath(), drainNode, p.nodeNameFor(h)))
	})
}

// nodeNameFor is the name h's node carries in the cluster, falling back to the name the
// machine reports when the nodes could not be listed at all. The fallback keeps the old
// behaviour for a cluster that cannot be read rather than silently acting on an empty name.
func (p *DeleteCommon) nodeNameFor(h *cluster.ZarfHost) string {
	if name, ok := p.nodeNames[h.String()]; ok {
		return name
	}
	return h.Configurer.Hostname(h)
}

func (p *DeleteCommon) deleteNode(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("deleting", "node", h)
	err := retry.Timeout(ctx, 10*time.Second, func(_ context.Context) error {
		return p.leader.Sudo().Exec(p.Distro.KubectlCmdf(p.leader, p.Distro.DataDirPath(), deleteNode, p.nodeNameFor(h)))
	})
	if err != nil {
		logger.From(ctx).Warn("got an error well deleting the", "node", h.Configurer.Hostname(h))
	}
	return nil
}
