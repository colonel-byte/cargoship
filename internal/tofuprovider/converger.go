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

package tofuprovider

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/internal/riglogger"
	"github.com/colonel-byte/cargoship/pkg/action"
	"github.com/colonel-byte/cargoship/pkg/phase"
)

// hostFacts is what the read-only phases learned about one host.
//
// EngineVersion is the attribute the plan-time downgrade check of #307 compares against, which is
// why it is in the schema before there is anything to compare: an attribute that does not exist
// cannot be compared later. See docs/agent/choice-tofu-provider-layout.md.
type hostFacts struct {
	Address        string
	Hostname       string
	OS             string
	OSVersion      string
	Arch           string
	PrivateAddress string
	Role           string
	EngineVersion  string
}

// converger is everything the provider asks of cargoship.
//
// It exists as an interface so the resources and data sources can be tested without SSH, a
// cluster, or a distro package: the real implementation builds a phase.Manager and runs an action,
// and a fake one answers from a table. Provider bugs live in state mapping and diagnostics rather
// than in the phases, and those are only testable if this seam exists.
type converger interface {
	// Refresh runs the read-only phases and reports what they found, host by host. It changes
	// nothing, which is what lets a data source and a resource's Read share it.
	Refresh(ctx context.Context, c *cluster.ZarfCluster, distroID string) ([]hostFacts, error)
}

// DefaultConnectTimeout bounds how long a read waits for a fleet.
//
// It exists because the connect phase is built for the CLI, where a host rebooting into a new
// kernel is ordinary: it retries for ten minutes before giving up (pkg/phase/07_connect.go). That
// is the right answer for an apply somebody is watching and the wrong one for a plan, which would
// sit silent for ten minutes on a typo in an address. A plan that fails in a minute with the
// address in the message is more useful than one that eventually fails with the same message.
const DefaultConnectTimeout = time.Minute

// cargoshipConverger is the real implementation: an in-process phase.Manager, built the way the
// CLI builds one.
type cargoshipConverger struct {
	// concurrency caps how many hosts a phase acts on at once. Zero means unlimited.
	concurrency int
	// connectTimeout bounds a read, DefaultConnectTimeout when zero.
	connectTimeout time.Duration
	// out is where a phase's own progress writing goes. A provider has no terminal, so the
	// default is io.Discard and the log lines reach tofu through the logger instead.
	out io.Writer
}

// Refresh runs action.NewRefresh, which is the read-only half of an apply: it connects, resolves
// each host's OS, gathers the network facts and reads the engine version already installed, and
// disconnects. Every phase in it declares ReadOnly() and no cluster lock is taken, which is what
// makes it safe during a plan -- and it lives in pkg/action rather than here so that "which phases
// are safe to run" is decided in one place rather than two.
func (c cargoshipConverger) Refresh(ctx context.Context, cfg *cluster.ZarfCluster, distroID string) ([]hostFacts, error) {
	if distroID == "" {
		return nil, fmt.Errorf("distro is required: the engine to read, %q or %q", "k3s", "rke2")
	}

	out := c.out
	if out == nil {
		out = io.Discard
	}

	refresh, err := action.NewRefresh(action.RefreshOptions{
		Manager: &phase.Manager{
			Config:            cfg,
			DistroID:          distroID,
			Concurrency:       c.concurrency,
			ConcurrentUploads: c.concurrency,
			Writer:            out,
		},
	})
	if err != nil {
		return nil, err
	}

	// rig logs through its own logger; routing it onto the context's logger is what puts an SSH
	// failure in the provider's diagnostics rather than nowhere.
	if err := riglogger.RigLogger(ctx); err != nil {
		return nil, fmt.Errorf("unable to route the SSH logs: %w", err)
	}

	timeout := c.connectTimeout
	if timeout <= 0 {
		timeout = DefaultConnectTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := refresh.Run(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("gave up after %s: %w (raise connect_timeout if the fleet is slow to answer)", timeout, err)
		}
		return nil, err
	}
	return factsOf(cfg.Spec.Hosts), nil
}

// factsOf reads the facts the phases left on the hosts.
//
// The phases record what they learn on the host objects themselves, so this is read after the run
// rather than collected during it. A host that was never reached carries zero values, which is the
// honest answer: the run failed, and the caller is about to report that.
//
// The OS release is read through host.OS() rather than off the metadata, because that is where
// DetectOS caches it -- and it is read only when the host has one, since the accessor would
// otherwise try to detect it over a connection the disconnect phase has already closed.
func factsOf(hosts cluster.ZarfHosts) []hostFacts {
	facts := make([]hostFacts, 0, len(hosts))
	for _, host := range hosts {
		fact := hostFacts{
			Address:        addressOf(host),
			Hostname:       host.Metadata.Hostname,
			Arch:           host.Metadata.Arch,
			PrivateAddress: host.PrivateAddress,
			Role:           host.Role,
			EngineVersion:  host.Metadata.DistroVersion,
		}
		if host.OSRelease != nil {
			fact.OS = host.OSRelease.ID
			fact.OSVersion = host.OSRelease.Version
		}
		facts = append(facts, fact)
	}
	return facts
}

// addressOf is the address a configuration would recognise: what the host block said, with the
// port when it is not the default.
//
// rig's CompositeConfig.String renders a Go-ish "ssh.Config{127.0.0.1:2223}", which is right for a
// log line and wrong for an attribute somebody writes a comparison against. ZarfHost.Address is
// not an option either: it reads through the embedded rig client, which a host that has already
// been disconnected no longer has.
func addressOf(host *cluster.ZarfHost) string {
	cfg := host.ConnectionConfig.SSH
	if cfg == nil {
		return host.ConnectionConfig.String()
	}
	if cfg.Port == 0 || cfg.Port == defaultSSHPort {
		return cfg.Address
	}
	return net.JoinHostPort(cfg.Address, strconv.Itoa(cfg.Port))
}
