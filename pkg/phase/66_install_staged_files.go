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

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// InstallStagedFiles runs the install the upload phases staged on any host where nothing else
// ran it.
//
// The upload phases do not install what they upload. They hand each host a closure -- see
// BINUploadFiles, RPMUploadFiles and APTUploadFiles -- and the initialize and upgrade phases call
// it, which is deliberate: an executable is uploaded to "<target>.tmp.<nonce>" and the closure is
// what moves it into place, so a rolling upgrade replaces a running engine binary on one host at
// a time, immediately before that host restarts onto it.
//
// What that leaves is a host no phase acts on. InitializeControllers, InitializeWorkers and
// UpgradeHosts are each gated on having a host to initialize or upgrade, so a converge of a
// cluster already running the package's version calls none of them -- while the upload phases
// have no such gate and stage regardless. The host was left with the new engine binary under its
// .tmp.<nonce> name and nothing at the path its unit file names: a cluster that keeps running,
// because the engine was already exec'd, and that cannot be restarted. Nothing reported it.
//
// This phase closes that gap without moving the install earlier. It runs last of the install
// phases, so every host the initialize and upgrade phases claimed has already consumed its
// closure (installPackages clears it), and only a host nothing else installed is left to act on.
type InstallStagedFiles struct {
	GenericPhase
}

// Title for the phase
func (p *InstallStagedFiles) Title() string {
	return "Install staged files"
}

// Explanation about the current phase, used for documentation generation
func (p *InstallStagedFiles) Explanation() string {
	return "Moves staged engine files into place on any host the initialize and upgrade phases did not install"
}

// ShouldRun is true when a host is still carrying an install nothing has run.
func (p *InstallStagedFiles) ShouldRun() bool {
	return len(p.pending()) > 0
}

// Run the phase
func (p *InstallStagedFiles) Run(ctx context.Context) error {
	return p.parallelDo(ctx, p.pending(), func(ctx context.Context, h *cluster.Host) error {
		logger.From(ctx).Info("installing the files this run staged", "host", h)
		if err := p.installPackages(ctx, h); err != nil {
			logger.From(ctx).Warn("failed to install the staged files", "host", h, "error", err)
		}
		return nil
	})
}

// pending are the hosts still carrying an install closure nothing has run.
func (p *InstallStagedFiles) pending() cluster.Hosts {
	return p.manager.Config.Spec.Hosts.Filter(func(h *cluster.Host) bool {
		return h.Metadata.Install != nil
	})
}
