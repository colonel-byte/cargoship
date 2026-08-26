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
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/pkg/retry"
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

// Explanation about the current phase, used for documentation generation
func (p *Lock) Explanation() string {
	return "Runs a background task that will touch a file every 30 seconds on each remote node, this prevents other `cargoships` from doing any changes until the lock file has not been touch for over a minute"
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
				if err := h.Sudo().FS().Touch(lfp, time.Now()); err != nil {
					logger.From(ctx).Debug("failed to touch lock file", "host", h, "error", err)
				}
			case <-ctx.Done():
				logger.From(ctx).Debug("fstopped lock cycle, removing file", "host", h)
				if err := h.DeleteFile(lfp); err != nil {
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
	return retry.Times(ctx, 10, func(ctx context.Context) error {
		return p.tryLock(ctx, h)
	})
}

// tryLock atomically creates the lock file with O_EXCL so a second cargoship
// instance can never observe a half-written file. All reads/writes here use
// h.Sudo().FS() -- the lock file may live in a root-owned directory
// (/run/lock), and dropping sudo on any of these calls would silently fail
// the existence/ownership checks below instead of erroring loudly.
func (p *Lock) tryLock(ctx context.Context, h *cluster.ZarfHost) error {
	lfp := h.Configurer.CTLLockFilePath(h)

	f, err := h.Sudo().FS().OpenFile(lfp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("failed to create lock file %s: %w", lfp, err)
		}

		// The file already exists -- check whether it belongs to us or is stale.
		stat, statErr := h.Sudo().FS().Stat(lfp)
		if statErr != nil {
			return fmt.Errorf("lock file disappeared: %w", statErr)
		}
		content, readErr := h.ReadFile(lfp)
		if readErr != nil {
			return fmt.Errorf("failed to read lock file:  %w", readErr)
		}
		if content != p.instanceID {
			if time.Since(stat.ModTime()) < 30*time.Second {
				return fmt.Errorf("another instance of cargoship is currently operating on the host, delete %s or wait 30 seconds for it to expire", lfp)
			}
			return h.DeleteFile(lfp)
		}
		return nil
	}

	if _, err := f.Write([]byte(p.instanceID)); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			logger.From(ctx).Debug("failed to close lock file after write error", "host", h, "error", closeErr)
		}
		if delErr := h.DeleteFile(lfp); delErr != nil {
			logger.From(ctx).Debug("failed to remove lock file after write error", "host", h, "error", delErr)
		}
		return fmt.Errorf("failed to write lock file: %w", err)
	}
	if err := f.Close(); err != nil {
		if delErr := h.DeleteFile(lfp); delErr != nil {
			logger.From(ctx).Debug("failed to remove lock file after close error", "host", h, "error", delErr)
		}
		return fmt.Errorf("failed to close lock file: %w", err)
	}

	return nil
}
