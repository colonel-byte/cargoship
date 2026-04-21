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
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/colonel-byte/mare/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/mare/src/api/zarf.dev/v1alpha1/distro"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

var (
	// Interval is the time to wait between retry attempts
	Interval = 10 * time.Second
)

type GenericPhase struct {
	manager *Manager
	wg      sync.WaitGroup
	m       sync.Mutex
}

// GetConfig is an accessor to phase Config
func (p *GenericPhase) GetConfig() *cluster.ZarfCluster {
	return p.manager.Config
}

// GetConfig is an accessor to phase Distro
func (p *GenericPhase) GetDistro() *distro.ZarfDistro {
	return p.manager.Distro
}

// Prepare the phase
func (p *GenericPhase) Prepare(c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.manager.Config = c
	p.manager.Distro = d
	return nil
}

// SetManager adds a reference to the phase manager
func (p *GenericPhase) SetManager(m *Manager) {
	p.manager = m
}

func (p *GenericPhase) parallelDo(ctx context.Context, hosts cluster.ZarfHosts, funcs ...func(context.Context, *cluster.ZarfHost) error) error {
	if p.manager.Concurrency == 0 {
		return hosts.ParallelEach(ctx, funcs...)
	}
	return hosts.BatchedParallelEach(ctx, p.manager.Concurrency, funcs...)
}

func (p *GenericPhase) parallelDoWithMessage(ctx context.Context, msg string, hosts cluster.ZarfHosts, funcs ...func(context.Context, *cluster.ZarfHost) error) (err error) {
	cancel, _ := p.tickerHelper(ctx, msg, Interval)
	defer cancel()
	return p.parallelDo(ctx, hosts, funcs...)
}

func (p *GenericPhase) parallelDoUpload(ctx context.Context, hosts cluster.ZarfHosts, funcs ...func(context.Context, *cluster.ZarfHost) error) error {
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

func (p *GenericPhase) batchedParallelWithMessageInterval(ctx context.Context, msg string, interval time.Duration, hosts cluster.ZarfHosts, batchSize int, funcs ...func(context.Context, *cluster.ZarfHost) error) (err error) {
	cancel, _ := p.tickerHelper(ctx, msg, interval)
	defer cancel()
	if batchSize <= 0 {
		return hosts.ParallelEach(ctx, funcs...)
	}
	return hosts.BatchedParallelEach(ctx, batchSize, funcs...)
}

func (p *GenericPhase) batchedParallelWithMessage(ctx context.Context, msg string, hosts cluster.ZarfHosts, batchSize int, funcs ...func(context.Context, *cluster.ZarfHost) error) (err error) {
	return p.batchedParallelWithMessageInterval(ctx, msg, Interval, hosts, batchSize, funcs...)
}

// Wet is a shorthand for manager.Wet
func (p *GenericPhase) Wet(host fmt.Stringer, msg string, funcs ...errorfunc) error {
	return p.manager.Wet(host, msg, funcs...)
}

func (p *GenericPhase) VersionLess(host *cluster.ZarfHost, version string) bool {
	con, err := semver.NewConstraint(fmt.Sprintf("< %s", strings.ReplaceAll(version, "+", "-")))
	if err != nil {
		return false
	}
	v, err := semver.NewVersion(strings.ReplaceAll(host.Metadata.DistroVersion, "+", "-"))
	if err != nil {
		return false
	}
	return con.Check(v)
}

func (p *GenericPhase) VersionGreater(host *cluster.ZarfHost, version string) bool {
	con, err := semver.NewConstraint(fmt.Sprintf("> %s", strings.ReplaceAll(version, "+", "-")))
	if err != nil {
		return false
	}
	v, err := semver.NewVersion(strings.ReplaceAll(host.Metadata.DistroVersion, "+", "-"))
	if err != nil {
		return false
	}
	return con.Check(v)
}

func (p *GenericPhase) tickerHelper(ctx context.Context, msg string, interval time.Duration) (context.CancelFunc, error) {
	ticker := time.NewTicker(interval)
	start := time.Now()
	child, cancel := context.WithTimeout(ctx, p.manager.Timeout)

	p.wg.Add(1)
	go func() {
		for {
			select {
			case <-ticker.C:
				logger.From(child).Info(msg, "time", time.Since(start).Truncate(time.Second))
			case <-child.Done():
				logger.From(child).Info(fmt.Sprintf("completed task: %s", msg), "time", time.Since(start).Truncate(time.Second))
				return
			}
		}
	}()

	return cancel, nil
}
