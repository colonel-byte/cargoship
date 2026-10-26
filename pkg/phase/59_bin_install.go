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
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package phase

import (
	"context"
	"fmt"

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// BINUploadFiles implements a phase which upload files to hosts
type BINUploadFiles struct {
	UploadFilesCommon
	Distro distrocfg.Distro
}

// Title for the phase
func (p *BINUploadFiles) Title() string {
	return "Upload files to hosts -- Binaries"
}

// Explanation about the current phase, used for documentation generation
func (p *BINUploadFiles) Explanation() string {
	return "Catch-all phase for when the combination of Operating System and Distro has no other install method"
}

// Prepare the phase
func (p *BINUploadFiles) Prepare(ctx context.Context, c *cluster.Cluster, d *distro.Distro) error {
	if err := p.UploadFilesCommon.Prepare(ctx, c, d); err != nil {
		logger.From(ctx).Warn("got", "error", err)
	}

	p.filesControl = p.getProfileFiles(ctx, config.SelectorBIN, cluster.RoleController)
	p.filesWorkers = p.getProfileFiles(ctx, config.SelectorBIN, cluster.RoleWorker)

	return nil
}

// Run the phase
//
// The upload runs first, then every staged binary is moved into place. Promotion used to be
// deferred into h.Metadata.Install, which only the initialize and upgrade phases call, and both
// of those are gated on having a host to initialize or upgrade -- so a converge of a cluster
// already running the package's version staged a new engine binary and promoted nothing. The host
// was left with no file at the path its unit names and an orphaned .tmp.<nonce> beside it: a
// cluster that keeps running, because the engine was already exec'd, and cannot be restarted.
//
// Staging and promoting are one step here so neither can happen without the other. The upload
// stages rather than writing over the destination so that replacing a running engine binary is an
// atomic rename (see stageTempPath), and that property does not depend on when the rename happens
// -- a running process keeps the inode it was started from either way.
func (p *BINUploadFiles) Run(ctx context.Context) error {
	if err := p.UploadFilesCommon.Run(ctx); err != nil {
		return err
	}

	if err := p.parallelDo(ctx, p.control, func(ctx context.Context, zh *cluster.Host) error {
		return p.promoteStaged(ctx, p.filesControl, zh)
	}); err != nil {
		logger.From(ctx).Warn("got", "error", err)
	}
	if err := p.parallelDo(ctx, p.workers, func(ctx context.Context, zh *cluster.Host) error {
		return p.promoteStaged(ctx, p.filesWorkers, zh)
	}); err != nil {
		logger.From(ctx).Warn("got", "error", err)
	}

	return nil
}

// promoteStaged moves each of h's staged executables from its .tmp.<nonce> name to the target the
// package named. A file the upload did not stage has OriginalTarget equal to Target and is already
// where it belongs.
//
// A failed move is warned about rather than returned, matching the rest of this phase: the next
// run stages and promotes again, and failing the whole apply over one host would leave the hosts
// that did promote without the configuration phases that follow.
func (p *BINUploadFiles) promoteStaged(ctx context.Context, byArch map[api.Arch][]v1alpha1.File, h *cluster.Host) error {
	files, err := p.filesFor(byArch, h)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		logger.From(ctx).Warn("the package carries no engine binaries for this host architecture", "host", h)
	}

	for _, f := range stagedFiles(files) {
		logger.From(ctx).Debug("installing binary", "target", f.OriginalTarget)
		if err := h.Sudo().Exec(fmt.Sprintf("mv %s %s", f.Target, f.OriginalTarget)); err != nil {
			logger.From(ctx).Warn("failed to move", "error", err)
		}
	}
	return nil
}

// stagedFiles are the files the upload wrote under a temporary name and so still have to be moved
// to the target the package named.
//
// A file written straight to its target carries OriginalTarget equal to Target, and one recorded
// with no OriginalTarget at all carries the empty string -- neither is a move, and the second
// would be a move onto "" if it were treated as one.
func stagedFiles(files []v1alpha1.File) []v1alpha1.File {
	staged := []v1alpha1.File{}
	for _, f := range files {
		if f.OriginalTarget == "" || f.OriginalTarget == f.Target {
			continue
		}
		staged = append(staged, f)
	}
	return staged
}
