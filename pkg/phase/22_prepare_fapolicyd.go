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
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	// FAPolicydRuleFile path on the host
	FAPolicydRuleFile = "/etc/fapolicyd/rules.d/31-cargoship.rules"
	// FAPolicydExecutableRuleFile path for automatically generated executable rules on the host
	FAPolicydExecutableRuleFile = "/etc/fapolicyd/rules.d/32-cargoship-executable.rules"
	// FAPOLICYD name of the service for fapolicyd
	FAPOLICYD = "fapolicyd"
)

// PrepareFapolicy installs required packages and so on on the hosts.
type PrepareFapolicy struct {
	GenericPhase
	// hostChanges carries Changed and HostsChanged. The rules file is written without reading
	// what was there, so a change here is reported without a diff -- see the package note on
	// HostChangeDetail.Diff.
	hostChanges
	fapolicydhosts cluster.Hosts
}

// Prepare the phase
func (p *PrepareFapolicy) Prepare(ctx context.Context, _ *cluster.Cluster, _ *distro.Distro) error {
	p.fapolicydhosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.Host) bool {
		return h.ServiceIsRunning(ctx, FAPOLICYD)
	})

	logger.From(ctx).Info("number of systems with fapolicy", "hosts", len(p.fapolicydhosts))
	p.warnOnMissingRules(ctx)

	return nil
}

// warnOnMissingRules says so when the hosts run fapolicyd and the package carries no rules for
// it, which is the one combination this phase can do nothing about.
//
// ShouldRun goes false in that case, so the phase is skipped with nothing written and nothing
// said. What follows is a cluster that installs and then cannot start a container: fapolicyd
// denies the execs the runtime makes out of its own content store, and the failure arrives much
// later as `runc create failed: unable to start container process: can't copy bootstrap data to
// pipe: write init-p: broken pipe`, which names neither fapolicyd nor the package. An upstream
// package carried no rules at all until the fleet showed exactly that.
func (p *PrepareFapolicy) warnOnMissingRules(ctx context.Context) {
	if len(p.fapolicydhosts) == 0 || p.manager.Distro == nil || p.manager.Distro.Spec.Config.OS.FAPolicyd != "" {
		return
	}

	// The rules cargoship generates for the package's own executables do not cover the container
	// runtime, so this stays a warning even when executableRules writes a file.
	logger.From(ctx).Warn("hosts run fapolicyd and this package declares no rules for it: "+
		"fapolicyd may deny the container runtime's own executions, which surfaces later as a "+
		"container that cannot start rather than as a policy refusal. Add an `os.fapolicyd` block to the "+
		"package, or stop fapolicyd on these hosts",
		"hosts", len(p.fapolicydhosts),
		"rules", FAPolicydRuleFile,
	)
}

// Title for the phase
func (p *PrepareFapolicy) Title() string {
	return "Prepare hosts - Enterprise Linux support - Fapolicyd"
}

// Explanation about the current phase, used for documentation generation
func (p *PrepareFapolicy) Explanation() string {
	return "Writes the distro-supplied FAPolicy rules to " + FAPolicydRuleFile + " and executable rules to " + FAPolicydExecutableRuleFile
}

// Run the phase
func (p *PrepareFapolicy) Run(ctx context.Context) error {
	return p.parallelDo(ctx, p.fapolicydhosts, p.prepareHost)
}

// ShouldRun is true when there is a host with selinux or fapolicyd on the hosts
func (p *PrepareFapolicy) ShouldRun() bool {
	if len(p.fapolicydhosts) == 0 || p.manager.Distro == nil {
		return false
	}
	return p.manager.Distro.Spec.Config.OS.FAPolicyd != "" || len(p.executableRules()) > 0
}

// executableRules allows every file the package marks executable to run. perm=any rather
// than perm=execute because a script is read by its interpreter, which fapolicyd sees as an
// open rather than an exec, and the package's executables include shell scripts.
func (p *PrepareFapolicy) executableRules() string {
	if p.manager.Distro == nil {
		return ""
	}

	targets := map[string]struct{}{}
	for _, f := range slices.Concat(p.manager.Distro.Spec.Config.Files, p.manager.Distro.Spec.Config.OS.Files) {
		if f.Executable && f.Target != "" {
			targets[f.Target] = struct{}{}
		}
	}

	var b strings.Builder
	for _, target := range slices.Sorted(maps.Keys(targets)) {
		fmt.Fprintf(&b, "allow perm=any all : path=%s\n", target)
	}
	return b.String()
}

func (p *PrepareFapolicy) prepareHost(ctx context.Context, h *cluster.Host) error {
	wroteAny := false
	if p.manager.Distro.Spec.Config.OS.FAPolicyd != "" {
		if err := h.WriteFile(FAPolicydRuleFile, p.manager.Distro.Spec.Config.OS.FAPolicyd, "0644"); err != nil {
			return err
		}
		p.changedFile(h, FAPolicydRuleFile, reasonOutOfSync, "")
		wroteAny = true
	}

	execRules := p.executableRules()
	if execRules != "" {
		if err := h.WriteFile(FAPolicydExecutableRuleFile, execRules, "0644"); err != nil {
			return err
		}
		p.changedFile(h, FAPolicydExecutableRuleFile, reasonOutOfSync, "")
		wroteAny = true
	}

	if wroteAny {
		logger.From(ctx).Info("wrote fapolicyd files, restarting service", "host", h)
		return h.RestartService(ctx, FAPOLICYD)
	}
	return nil
}
