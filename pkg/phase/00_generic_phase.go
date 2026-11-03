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

// Package phase is all the various phases used for bootstrapping a cluster.
// The phase files are named in a rough order used during an install;
// - 0x are for preconnection resources, along with ssh-ing into the node
// - 1x are used to gather information about the nodes in the cluster
// - 2x are for modifying host services, like firewalls, host files, and enabling kernel level modules
// - 5x are for staging files for the distro engine, e.i. rpm's, apt's, or binary files
// - 6x are for starting the engine or for upgrading an existing install
// - 8x are for resetting the engine from the nodes
// - 9x are last minute things and finally disconnecting from a node
// - ext are files that are currently not used but may be incorporated later
package phase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

var (
	// Interval is the time to wait between retry attempts
	Interval = 10 * time.Second
)

// GenericPhase state
type GenericPhase struct {
	manager *Manager
	wg      sync.WaitGroup
}

// GetDistro is an accessor to phase Distro
func (p *GenericPhase) GetDistro() *distro.Distro {
	return p.manager.Distro
}

// Prepare the phase
func (p *GenericPhase) Prepare(c *cluster.Cluster, d *distro.Distro) error {
	p.manager.Config = c
	p.manager.Distro = d
	return nil
}

// SetManager adds a reference to the phase manager
func (p *GenericPhase) SetManager(m *Manager) {
	p.manager = m
}

func (p *GenericPhase) parallelDo(ctx context.Context, hosts cluster.Hosts, funcs ...func(context.Context, *cluster.Host) error) error {
	if p.manager.Concurrency == 0 {
		return hosts.ParallelEach(ctx, funcs...)
	}
	return hosts.BatchedParallelEach(ctx, p.manager.Concurrency, funcs...)
}

func (p *GenericPhase) parallelDoWithMessage(ctx context.Context, msg string, hosts cluster.Hosts, funcs ...func(context.Context, *cluster.Host) error) (err error) {
	cancel, err := p.tickerHelper(ctx, msg, Interval)
	if err != nil {
		logger.From(ctx).Warn("got error well setting up ticket", "error", err)
	}
	defer cancel()
	return p.parallelDo(ctx, hosts, funcs...)
}

func (p *GenericPhase) parallelDoUpload(ctx context.Context, hosts cluster.Hosts, funcs ...func(context.Context, *cluster.Host) error) error {
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

func (p *GenericPhase) batchedParallelWithMessageInterval(ctx context.Context, msg string, interval time.Duration, hosts cluster.Hosts, batchSize int, funcs ...func(context.Context, *cluster.Host) error) (err error) {
	cancel, err := p.tickerHelper(ctx, msg, interval)
	if err != nil {
		logger.From(ctx).Warn("got error well setting up ticket", "error", err)
	}
	defer cancel()
	if batchSize <= 0 {
		return hosts.ParallelEach(ctx, funcs...)
	}
	return hosts.BatchedParallelEach(ctx, batchSize, funcs...)
}

func (p *GenericPhase) batchedParallelWithMessage(ctx context.Context, msg string, hosts cluster.Hosts, batchSize int, funcs ...func(context.Context, *cluster.Host) error) (err error) {
	return p.batchedParallelWithMessageInterval(ctx, msg, Interval, hosts, batchSize, funcs...)
}

// batchedParallelPerProfileWithMessage groups hosts by profile and runs funcs over every group
// concurrently, with each group independently respecting its own batch size. A group's batch
// size comes from its profile's Concurrency override (see Profiles.ResolveConcurrency),
// falling back to fallback for hosts with no profile or a profile that sets no override.
func (p *GenericPhase) batchedParallelPerProfileWithMessage(ctx context.Context, msg string, hosts cluster.Hosts, fallback string, funcs ...func(context.Context, *cluster.Host) error) error {
	profiles := p.manager.Config.Spec.Config.Profiles
	groups := hosts.GroupByProfile()

	batchSizes := make([]int, len(groups))
	for i, group := range groups {
		batchSize, err := profiles[group.Profile].ResolveConcurrency(len(group.Hosts), fallback)
		if err != nil {
			return fmt.Errorf("profile %q: %w", group.Profile, err)
		}
		batchSizes[i] = batchSize
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error

	for i, group := range groups {
		groupMsg := msg
		if group.Profile != "" {
			groupMsg = fmt.Sprintf("%s (profile: %s)", msg, group.Profile)
		}

		wg.Add(1)
		go func(group cluster.HostGroup, batchSize int, msg string) {
			defer wg.Done()
			if err := p.batchedParallelWithMessage(ctx, msg, group.Hosts, batchSize, funcs...); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("profile %q: %w", group.Profile, err))
				mu.Unlock()
			}
		}(group, batchSizes[i], groupMsg)
	}

	wg.Wait()

	return errors.Join(errs...)
}

// Wet is a shorthand for manager.Wet
func (p *GenericPhase) Wet(f errorfunc) error {
	return p.manager.Wet(f)
}

// VersionLess if host version is less then the distro version
func (p *GenericPhase) VersionLess(host *cluster.Host, version string) bool {
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

// VersionGreater if host version is greater then the distro version
func (p *GenericPhase) VersionGreater(host *cluster.Host, version string) bool {
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

// VersionSkewTooGreat reports whether host's running version is more than one minor version
// behind target. kubeadm's own upgrade path refuses to skip a minor version, and a two-minor jump
// fails partway through the control plane rather than up front, so this catches it before a real
// run starts one. UnknownVersion (no engine detected yet, a fresh install) never counts as skew.
func (p *GenericPhase) VersionSkewTooGreat(host *cluster.Host, target string) bool {
	if host.Metadata.DistroVersion == UnknownVersion {
		return false
	}
	cur, err := semver.NewVersion(strings.ReplaceAll(host.Metadata.DistroVersion, "+", "-"))
	if err != nil {
		return false
	}
	tgt, err := semver.NewVersion(strings.ReplaceAll(target, "+", "-"))
	if err != nil {
		return false
	}
	return tgt.Major() != cur.Major() || tgt.Minor()-cur.Minor() > 1
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
