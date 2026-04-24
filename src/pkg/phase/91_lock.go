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
	"os"
	"sync"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/pkg/retry"
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// Lock phase state
type Lock struct {
	GenericPhase
	cfs        []func()
	instanceID string
	m          sync.Mutex
	wg         sync.WaitGroup
}

// Prepare the phase
func (p *Lock) Prepare(ctx context.Context, c *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	p.manager.Config = c
	hn, err := os.Hostname()
	if err != nil {
		hn = "unknown"
	}
	p.instanceID = fmt.Sprintf("%s-%d", hn, os.Getpid())
	logger.From(ctx).Debug("host instance id", "host", hn, "pid", p.instanceID)
	return nil
}

// Title for the phase
func (p *Lock) Title() string {
	return "Acquire exclusive host lock"
}

// Cancel releases the lock
func (p *Lock) Cancel(_ context.Context) {
	p.m.Lock()
	defer p.m.Unlock()
	for _, f := range p.cfs {
		f()
	}
	p.wg.Wait()
}

// CleanUp calls Cancel to release the lock
func (p *Lock) CleanUp() {
	p.Cancel(context.Background())
}

// UnlockPhase returns an unlock phase for this lock phase
func (p *Lock) UnlockPhase() Phase {
	return &Unlock{Cancel: p.Cancel}
}

// Run the phase
func (p *Lock) Run(ctx context.Context) error {
	if err := p.parallelDo(ctx, p.manager.Config.Spec.Hosts, p.startLock); err != nil {
		return err
	}
	return p.manager.Config.Spec.Hosts.ParallelEach(ctx, p.startTicker)
}

func (p *Lock) startTicker(ctx context.Context, h *cluster.ZarfHost) error {
	p.wg.Add(1)
	lfp := h.Configurer.CTLLockFilePath(h)
	ticker := time.NewTicker(10 * time.Second)
	ctx, cancel := context.WithCancel(ctx)
	p.m.Lock()
	p.cfs = append(p.cfs, cancel)
	p.m.Unlock()

	go func() {
		logger.From(ctx).Debug("started periodic update of lock file timestamp", "host", h, "lockfile", lfp)
		for {
			select {
			case <-ticker.C:
				if err := h.Configurer.Touch(h, lfp, time.Now()); err != nil {
					logger.From(ctx).Debug("failed to touch lock file", "host", h, "error", err)
				}
			case <-ctx.Done():
				logger.From(ctx).Debug("fstopped lock cycle, removing file", "host", h)
				if err := h.Configurer.DeleteFile(h, lfp); err != nil {
					logger.From(ctx).Debug("failed to remove host lock file, may have been previously aborted or crashed. the start of next invocation may be delayed until it expires", "host", h, "error", err)
				}
				p.wg.Done()
				return
			}
		}
	}()

	return nil
}

func (p *Lock) startLock(ctx context.Context, h *cluster.ZarfHost) error {
	return retry.Times(ctx, 10, func(_ context.Context) error {
		return p.tryLock(h)
	})
}

func (p *Lock) tryLock(h *cluster.ZarfHost) error {
	lfp := h.Configurer.CTLLockFilePath(h)

	if err := h.Configurer.UpsertFile(h, lfp, p.instanceID); err != nil {
		stat, err := h.Configurer.Stat(h, lfp, exec.Sudo(h), exec.HideCommand())
		if err != nil {
			return fmt.Errorf("lock file disappeared: %w", err)
		}
		content, err := h.Configurer.ReadFile(h, lfp)
		if err != nil {
			return fmt.Errorf("failed to read lock file:  %w", err)
		}
		if content != p.instanceID {
			if time.Since(stat.ModTime()) < 30*time.Second {
				return fmt.Errorf("another instance of k0sctl is currently operating on the host, delete %s or wait 30 seconds for it to expire", lfp)
			}
			_ = h.Configurer.DeleteFile(h, lfp)
			return fmt.Errorf("removed existing expired lock file, will retry")
		}
	}

	return nil
}
