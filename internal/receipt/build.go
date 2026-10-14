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

package receipt

import (
	"os"
	"os/user"
	"runtime"
	"time"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/pkg/phase"
)

// Options is everything the caller knows that the run result does not.
type Options struct {
	// Command is the lifecycle command that ran: apply, prepare, reset, engine-config-sync.
	Command string
	// Args is the raw command line. It is redacted on the way in, not by the caller.
	Args []string
	// Version and Commit identify the cargoship that ran. They come from the config package
	// rather than being read here, so a test can pin them.
	Version string
	Commit  string
	// ConfigPath is the cluster configuration the run was given.
	ConfigPath string
	// Package describes the distro package, and is nil for a command that loads none.
	Package *Package
	// StartedAt is when the command began, which is earlier than the first phase.
	StartedAt time.Time
	// DryRun is whether the run was a dry run.
	DryRun bool
	// Observed is whether any run reached its phases, from phase.ResultSink.Observed. It is
	// passed rather than inferred because an empty RunResult and a run that never started look
	// identical from the result alone.
	Observed bool
	// Err is how the run ended, nil on success.
	Err error
}

// Build assembles the receipt from the run.
//
// It never fails. A receipt is written on the failure path, and a builder that could itself fail
// there would be one more reason for an operator to end up with no record of the run that most
// needed one -- so every field is best-effort, and an unknown is left empty rather than reported.
func Build(cfg *cluster.Cluster, result phase.RunResult, opts Options) Receipt {
	finished := time.Now()

	r := Receipt{
		SchemaVersion: SchemaVersion,
		Invocation: Invocation{
			Command:    opts.Command,
			Args:       redactArgs(opts.Args),
			Version:    opts.Version,
			Commit:     opts.Commit,
			Operator:   operator(),
			Host:       managementHost(),
			Platform:   runtime.GOOS + "/" + runtime.GOARCH,
			DryRun:     opts.DryRun,
			StartedAt:  opts.StartedAt.UTC(),
			FinishedAt: finished.UTC(),
			Duration:   durationOf(finished.Sub(opts.StartedAt)),
		},
		Package: opts.Package,
		Phases:  phasesOf(result),
		Outcome: outcomeOf(result, opts),
	}

	if cfg != nil {
		r.Cluster = Cluster{
			Name:         cfg.Metadata.Name,
			LoadBalancer: cfg.Spec.Config.LoadBalancer,
			ConfigPath:   opts.ConfigPath,
			HostCount:    len(cfg.Spec.Hosts),
		}
		r.Hosts = hostsOf(cfg.Spec.Hosts, result)
	}

	// Both lists are always present and never null. A receipt is parsed by things cargoship
	// knows nothing about, and in several languages iterating a null is an error where
	// iterating an empty list is not -- so a run with no phases says so with [].
	if r.Phases == nil {
		r.Phases = []Phase{}
	}
	if r.Hosts == nil {
		r.Hosts = []Host{}
	}

	return r
}

// hostsOf records every host in the configuration, whether or not a phase spoke for it.
//
// Starting from the configuration rather than from what the phases reported is deliberate. A host
// that no phase named is exactly the host worth seeing in a failed run -- it is the one the run
// never reached -- and a receipt built only from what was reported would silently omit it.
func hostsOf(hosts cluster.Hosts, result phase.RunResult) []Host {
	if len(hosts) == 0 {
		return nil
	}

	summaries := map[string]phase.HostSummary{}
	for _, s := range result.HostSummaries() {
		summaries[s.Host] = s
	}

	out := make([]Host, 0, len(hosts))
	for _, h := range hosts {
		if h == nil {
			continue
		}
		address := h.ConfiguredAddress()
		rec := Host{
			Address:          address,
			Hostname:         h.Metadata.Hostname,
			Role:             h.Role,
			Profile:          h.Profile,
			Arch:             h.Metadata.Arch,
			PrivateAddress:   h.PrivateAddress,
			PrivateInterface: h.PrivateInterface,
			EngineVersion:    h.Metadata.DistroVersion,
		}
		// Read through OSRelease rather than the OS accessor: the accessor would try to detect
		// the release over a connection the disconnect phase has already closed.
		if h.OSRelease != nil {
			rec.OS = h.OSRelease.ID
			rec.OSVersion = h.OSRelease.Version
		}
		if s, ok := summaries[address]; ok {
			rec.Changed = s.Changed
			rec.PhasesNamed = s.Named
			rec.PhasesReporting = s.Reporting
		}
		out = append(out, rec)
	}
	return out
}

// outcomeOf reports how the run ended and how much its changed answer is worth.
func outcomeOf(result phase.RunResult, opts Options) Outcome {
	o := Outcome{
		Succeeded:       opts.Err == nil,
		Changed:         result.Changed(),
		ChangedSignal:   result.Signal(),
		HostsUndeclared: result.HostsUndeclared(),
	}
	if opts.Err != nil {
		o.Error = opts.Err.Error()
	}
	if !result.Complete() {
		o.ChangedUndeclared = result.Undeclared()
	}

	// A run that never reached its phases knows nothing, which is a different answer from a run
	// in which nothing changed. Saying partial there would name every phase as undeclared and
	// imply the run got to them.
	if !opts.Observed {
		o.Changed = false
		o.ChangedSignal = phase.SignalUnknown
		o.ChangedUndeclared = nil
		o.HostsUndeclared = nil
	}

	// A dry run changes nothing by construction. What it has instead is whether a real run
	// would, which is what Outstanding answers.
	if opts.DryRun && opts.Observed {
		o.Changed = result.Outstanding()
	}
	return o
}

// operator is the user cargoship ran as, for attribution.
func operator() string {
	if u, err := user.Current(); err == nil && u != nil {
		return u.Username
	}
	// os/user needs cgo or a parseable passwd file, neither guaranteed on a stripped
	// management image. The environment is the fallback an auditor would check anyway.
	for _, key := range []string{"USER", "LOGNAME", "USERNAME"} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
}

// managementHost is the node the run was driven from. Cargoship opens the SSH connections itself,
// so this is not one of the managed hosts -- it is the machine an auditor would go to for the
// rest of the evidence.
func managementHost() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}
