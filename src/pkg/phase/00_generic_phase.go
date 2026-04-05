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

	v1alpha1 "github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
)

type GenericPhase struct {
	Config  *v1alpha1.ZarfCluster
	manager *Manager
}

// GetConfig is an accessor to phase Config
func (p *GenericPhase) GetConfig() *v1alpha1.ZarfCluster {
	return p.Config
}

// Prepare the phase
func (p *GenericPhase) Prepare(c *v1alpha1.ZarfCluster) error {
	p.Config = c
	return nil
}

// SetManager adds a reference to the phase manager
func (p *GenericPhase) SetManager(m *Manager) {
	p.manager = m
}

func (p *GenericPhase) parallelDo(ctx context.Context, hosts v1alpha1.ZarfHosts, funcs ...func(context.Context, *v1alpha1.ZarfHost) error) error {
	if p.manager.Concurrency == 0 {
		return hosts.ParallelEach(ctx, funcs...)
	}
	return hosts.BatchedParallelEach(ctx, p.manager.Concurrency, funcs...)
}

func (p *GenericPhase) parallelDoUpload(ctx context.Context, hosts v1alpha1.ZarfHosts, funcs ...func(context.Context, *v1alpha1.ZarfHost) error) error {
	if p.manager.Concurrency == 0 {
		return hosts.ParallelEach(ctx, funcs...)
	}

	batchSize := p.manager.ConcurrentUploads
	if batchSize <= 0 {
		batchSize = p.manager.Concurrency
	} else {
		batchSize = min(batchSize, p.manager.Concurrency)
	}

	return hosts.BatchedParallelEach(ctx, batchSize, funcs...)
}

// // runHooks executes hooks for the provided hosts honoring the given context.
// func (p *GenericPhase) runHooks(ctx context.Context, action, stage string, hosts ...*v1alpha1.ZarfHost) error {
// 	return p.parallelDo(ctx, hosts, func(_ context.Context, h *v1alpha1.ZarfHost) error {
// 		if !p.IsWet() {
// 			// In dry-run, list each hook command that would be executed.
// 			cmds := h.Hooks.ForActionAndStage(action, stage)
// 			for _, cmd := range cmds {

// 				p.DryMsgf(h, "run %s %s hook: %q", stage, action, cmd)
// 			}
// 			return nil
// 		}

// 		if err := h.RunHooks(ctx, action, stage); err != nil {
// 			return fmt.Errorf("running hooks failed: %w", err)
// 		}

// 		return nil
// 	})
// }
