// Copyright 2023 k0sctl authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
//
// Modifications Copyright 2026 colonel-byte.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//

package phase

import (
	"context"
	"fmt"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	goyaml "github.com/goccy/go-yaml"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// keyServer is the config.yaml key naming the controller a node registers with. It is spelled
// here as well as in types/distrocfg because this phase edits a file that package wrote, and
// the two have to agree on the key for that to work.
const keyServer = "server"

// ReconfigureSurvivors repoints the controllers a removal kept at a controller it did not
// remove.
//
// A controller that is not the first one carries `server:` naming the first, written when it
// joined. That value is read at startup, so removing the first controller leaves a cluster
// that keeps working and cannot come back: every survivor names a machine that is gone, and
// the next restart or reboot of any of them fails to rejoin.
//
// cargoship apply already fixes this -- it re-elects the first controller from the hosts that
// are left and rewrites `server:` on the rest -- and the documented procedure is to run one
// after a removal. The trouble with leaving it there is that a reset reports success having
// created that state, so the safety of the operation rests on the operator remembering a second
// command. This phase converges the survivors itself, and says so when it cannot.
//
// It edits the file in place rather than rendering a fresh one, because a reset loads no
// package and so has nothing to render from. Only `server:` is touched.
type ReconfigureSurvivors struct {
	GenericPhase
	// hostChanges carries Changed and HostsChanged. This phase reads the engine config before
	// rewriting it, so it reports a real diff.
	hostChanges
	Distro      distrocfg.Distro
	TargetHosts []string

	// removed are the controllers this run is taking out, and survivors the controllers it is
	// keeping. leader is the survivor that becomes the first controller.
	removed   cluster.Hosts
	survivors cluster.Hosts
	leader    *cluster.Host
	// leaderURL is what the other survivors should name, resolved once rather than per host:
	// it needs the leader's configurer, which only exists after the OS has been detected.
	leaderURL string
	// unsupported is set when the distro has no `server:` key for this phase to rewrite, which
	// is a reason to do nothing rather than a reason to do it wrong. See Prepare.
	unsupported bool
}

// Title for the phase
func (p *ReconfigureSurvivors) Title() string {
	return "Repoint the controllers that were kept"
}

// Explanation about the current phase, used for documentation generation
func (p *ReconfigureSurvivors) Explanation() string {
	return "When a removal takes a controller out, rewrites `server:` on the controllers that stayed so they name one that is still there, which is what lets them rejoin after a restart"
}

// Prepare the phase
func (p *ReconfigureSurvivors) Prepare(ctx context.Context, _ *cluster.Cluster, _ *distro.Distro) error {
	hosts := p.manager.Config.Spec.Hosts
	controllers := hosts.Filter(func(h *cluster.Host) bool { return h.IsController() })

	for _, h := range controllers {
		if matchesTargetHost(h, p.TargetHosts) {
			p.removed = append(p.removed, h)
			continue
		}
		p.survivors = append(p.survivors, h)
	}

	// A distro that forms its cluster with a bootstrap command has no single config file with a
	// `server:` key in it, so there is nothing here to rewrite. kubeadm is the one such distro
	// today: its kubeadm-config.yaml is a multi-document file -- ClusterConfiguration, Init or
	// JoinConfiguration, KubeletConfiguration -- and none of them hold the endpoint a running
	// node uses, which lives in kubelet.conf and admin.conf. Editing it the way this phase edits
	// an rke2 config.yaml would read one document and write back one, dropping the other two.
	// Repointing a kubeadm survivor properly is https://github.com/colonel-byte/cargoship/issues/862.
	if _, ok := p.Distro.(distrocfg.Bootstrapper); ok {
		p.unsupported = true
		if len(p.removed) > 0 && len(p.survivors) > 0 {
			logger.From(ctx).Warn("this distro joins its nodes with a bootstrap command rather than a `server:` key, so the controllers that stayed are left naming whatever they already named; check them before they restart",
				"kept", len(p.survivors), "removed", len(p.removed))
		}
		return nil
	}

	// The first surviving controller becomes the leader, which is the same election
	// ConfigureEngine makes -- control[0] of the hosts it is given.
	if len(p.survivors) > 0 {
		p.leader = p.survivors[0]
		if p.leader.Configurer != nil {
			p.leaderURL = p.Distro.SupervisorURL(p.leader.Configurer.LongHostname(p.leader))
		}
	}
	logger.From(ctx).Debug("controllers after the removal", "removed", len(p.removed), "kept", len(p.survivors))
	return nil
}

// ShouldRun is true when the run removed a controller and kept at least one, on a distro this
// phase knows how to repoint.
//
// Removing every controller is a whole-cluster teardown, where there is nothing to repoint. An
// empty target list means every host, which is that same case.
func (p *ReconfigureSurvivors) ShouldRun() bool {
	return !p.unsupported && len(p.removed) > 0 && len(p.survivors) > 0
}

// Run the phase
func (p *ReconfigureSurvivors) Run(ctx context.Context) error {
	return p.parallelDo(ctx, p.survivors, p.repoint)
}

// repoint brings one surviving controller's `server:` to what it should be now.
//
// The leader carries no `server:` at all -- that is how ConfigureEngine writes it, and what
// makes it the first controller -- so on the leader the key is removed rather than rewritten.
func (p *ReconfigureSurvivors) repoint(ctx context.Context, h *cluster.Host) error {
	path := p.Distro.ConfigPath()

	// Failing here rather than warning past it: a survivor whose config cannot be read is a
	// survivor still naming a controller that was removed, and a reset that reports success
	// having left the cluster in that state is the failure shape #709 and #339 kept finding.
	content, err := h.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading the engine config on %s, so it still names a controller that was removed: %w", h, err)
	}

	cfg := map[string]any{}
	if err := goyaml.Unmarshal([]byte(content), &cfg); err != nil {
		logger.From(ctx).Warn("could not parse the engine config, so this host still names a controller that was removed; run `cargoship apply` before it restarts",
			"host", h, "path", path, "error", err)
		return nil
	}

	want, wantSet := p.serverFor(h)
	if wantSet && want == "" {
		logger.From(ctx).Warn("could not resolve the name of the controller that stayed, so this host still names one that was removed; run `cargoship apply` before it restarts",
			"host", h)
		return nil
	}
	if got, gotSet := cfg[keyServer]; gotSet == wantSet && got == want {
		p.observed(h)
		return nil
	}

	if wantSet {
		cfg[keyServer] = want
		logger.From(ctx).Info("repointing at the controller that stayed", "host", h, keyServer, want)
	} else {
		delete(cfg, keyServer)
		logger.From(ctx).Info("this host is the first controller now, so it names none", "host", h)
	}

	out, err := goyaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("rendering the engine config for %s: %w", h, err)
	}
	if err := h.WriteFile(path, string(out), "0600"); err != nil {
		return fmt.Errorf("writing the engine config on %s: %w", h, err)
	}
	p.changedFile(h, path, reasonOutOfSync, unifiedDiff(path, content, string(out)))
	return nil
}

// serverFor is the `server:` value a surviving controller should carry, and whether it should
// carry one at all.
func (p *ReconfigureSurvivors) serverFor(h *cluster.Host) (string, bool) {
	// Pointer identity, not Host.String(): the leader is an element of p.survivors, so
	// this is exact. String() is the connection address, which is the same for two hosts that
	// have not been dialled and would make one of them look like the leader.
	if p.leader == nil || h == p.leader {
		return "", false
	}
	return p.leaderURL, true
}
