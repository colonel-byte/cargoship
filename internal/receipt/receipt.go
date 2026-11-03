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

// Package receipt builds the structured record of one cargoship run.
//
// When cargoship runs as an Ansible module a playbook gets a structured result; when an operator
// runs the same lifecycle commands from the CLI the command returns nothing but an error, and
// everything the run knew is left in log lines on stderr. This is the artifact that closes that
// gap: one self-describing JSON document per run, carrying what was run, against which package,
// against which fleet, and what moved.
//
// It is written on failure as well as success. A failed apply is the run an auditor most wants a
// record of, and it is the run whose log lines are least likely to still be in a scrollback
// buffer by the time anyone asks.
package receipt

import (
	"time"

	"github.com/colonel-byte/cargoship/pkg/phase"
)

// SchemaVersion is the version of the receipt document format.
//
// It is first in the document and first here because a receipt outlives the cargoship that wrote
// it: these are kept for audit, parsed by things cargoship knows nothing about, and read back
// after an upgrade. Bump the minor for a field added, the major for a field removed or given a
// new meaning.
const SchemaVersion = "1.1"

// Receipt is the record of one cargoship run.
type Receipt struct {
	SchemaVersion string `json:"schemaVersion"`
	// Redacted marks a receipt that has been through the sanitizer for sharing. It is absent on
	// a full receipt rather than false, so that a reviewer handed a file can tell the two apart
	// at a glance instead of inferring it from which fields happen to be missing.
	Redacted   bool       `json:"redacted,omitempty"`
	Invocation Invocation `json:"invocation"`
	// Package is absent for a command that loads none, which is what reset and kube-config do.
	Package *Package `json:"package,omitempty"`
	Cluster Cluster  `json:"cluster"`
	Phases  []Phase  `json:"phases"`
	Hosts   []Host   `json:"hosts"`
	Outcome Outcome  `json:"outcome"`
}

// Invocation is what was run, by whom, from where.
type Invocation struct {
	Command string `json:"command"`
	// Args is the command line as given, with the value of any flag known to carry a secret
	// replaced. See redactArgs for why that is a shorter list than it looks.
	Args       []string  `json:"args,omitempty"`
	Version    string    `json:"version,omitempty"`
	Commit     string    `json:"commit,omitempty"`
	Operator   string    `json:"operator,omitempty"`
	Host       string    `json:"host,omitempty"`
	Platform   string    `json:"platform,omitempty"`
	DryRun     bool      `json:"dryRun"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	// Duration is the whole run's wall clock, which is not the sum of the phase durations: it
	// includes loading the package, resolving values and connecting, none of which is a phase.
	Duration string `json:"duration,omitempty"`
}

// Package is the provenance of the distro package the run applied.
//
// There is no SBOM field. Cargoship does not produce one yet -- package creation hardcodes
// SkipSBOM -- and a field that is always absent is worse than no field, because it reads as "this
// package had none" rather than "nothing here records that".
type Package struct {
	Path string `json:"path,omitempty"`
	Name string `json:"name,omitempty"`
	// Version is the package's own version, not cargoship's.
	Version string `json:"version,omitempty"`
	// SHA256 is the digest of the archive as given, present only when the package was loaded
	// from a local file. It is what an auditor compares against the artifact they were shipped.
	SHA256 string `json:"sha256,omitempty"`
	// Digest is the OCI manifest digest computed over the package contents at load. It differs
	// from SHA256 -- that is the bytes of one archive, this is the identity of what is inside
	// it, and the same contents repacked keep this and change that.
	Digest       string `json:"digest,omitempty"`
	Architecture string `json:"architecture,omitempty"`
	Engine       string `json:"engine,omitempty"`
	Signed       bool   `json:"signed"`
	// Verification is the strategy the run was asked to apply: never, if-possible, or always.
	// Signed says the package carried a signature; this says whether anyone checked it.
	Verification string `json:"verification,omitempty"`
}

// Cluster is the fleet the run was pointed at.
type Cluster struct {
	Name string `json:"name,omitempty"`
	// LoadBalancer is the cluster's control-plane endpoint, as configured.
	LoadBalancer string `json:"loadBalancer,omitempty"`
	ConfigPath   string `json:"configPath,omitempty"`
	HostCount    int    `json:"hostCount"`
}

// Phase is one phase of the run.
type Phase struct {
	Title       string `json:"title"`
	Explanation string `json:"explanation,omitempty"`
	// Disposition is one of DispositionRan, DispositionPlanned or DispositionSkipped.
	Disposition string `json:"disposition"`
	// DryRun is what a dry run does with this phase, whether or not this run was one.
	DryRun string `json:"dryRun,omitempty"`
	// Declared is whether the phase said anything about changing the fleet. When it is false,
	// Changed is false because nothing was asked, not because nothing happened.
	Declared bool `json:"declared"`
	Changed  bool `json:"changed"`
	// HostsDeclared stands to Hosts as Declared stands to Changed.
	HostsDeclared bool         `json:"hostsDeclared"`
	Hosts         []HostChange `json:"hosts,omitempty"`
	Duration      string       `json:"duration,omitempty"`
}

// Phase dispositions: where a phase ended up.
const (
	// DispositionRan means the phase executed, as itself or by its own dry-run path.
	DispositionRan = "ran"
	// DispositionPlanned means a dry run reported the phase instead of running it.
	DispositionPlanned = "planned"
	// DispositionSkipped means ShouldRun filtered the phase out. It is not the same as doing
	// nothing: a phase can finish its work in Prepare and then have nothing left to run.
	DispositionSkipped = "skipped"
)

// HostChange is what one phase did to one host.
type HostChange struct {
	Host    string             `json:"host"`
	Changed bool               `json:"changed"`
	Details []HostChangeDetail `json:"details,omitempty"`
}

// HostChangeDetail is one thing a phase did to one host.
type HostChangeDetail struct {
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Diff can carry anything the file carried, which for an engine configuration includes join
	// tokens and registry credentials. The sanitizer drops it outright; nothing that leaves the
	// management node should contain it.
	Diff string `json:"diff,omitempty"`
}

// Host is one host of the fleet, as the run found it.
//
// Every field here is copied in by name. Nothing marshals cluster.Host directly and nothing
// should: it embeds a live rig client and a metadata struct full of connection state, so a
// receipt built by excluding the fields that must not appear would leak the next field somebody
// adds. An allowlist fails closed -- a new fact is absent from receipts until someone adds it
// here, which is the safe direction to be wrong in.
type Host struct {
	Address          string `json:"address"`
	Hostname         string `json:"hostname,omitempty"`
	Role             string `json:"role,omitempty"`
	Profile          string `json:"profile,omitempty"`
	Arch             string `json:"arch,omitempty"`
	OS               string `json:"os,omitempty"`
	OSVersion        string `json:"osVersion,omitempty"`
	PrivateAddress   string `json:"privateAddress,omitempty"`
	PrivateInterface string `json:"privateInterface,omitempty"`
	EngineVersion    string `json:"engineVersion,omitempty"`
	// Changed is whether any phase that reports per host said this one moved.
	Changed bool `json:"changed"`
	// PhasesNamed is how many phases named this host and PhasesReporting how many declared per
	// host at all. The pair is reported because the numerator alone cannot be read: named by
	// two of two phases is complete coverage, named by two of fifteen is not, and they look the
	// same without the denominator.
	PhasesNamed     int `json:"phasesNamed"`
	PhasesReporting int `json:"phasesReporting"`
}

// Outcome is how the run ended.
type Outcome struct {
	Succeeded bool   `json:"succeeded"`
	Error     string `json:"error,omitempty"`
	Changed   bool   `json:"changed"`
	// ChangedSignal says how much Changed is worth: complete when every phase the run reached
	// said whether it changed anything, partial when some did not, unknown when the run never
	// reached its phases.
	ChangedSignal string `json:"changedSignal"`
	// ChangedUndeclared names the phases that did not say, and HostsUndeclared those that did
	// not say per host. They are what make a partial signal actionable rather than a warning
	// with nothing behind it.
	ChangedUndeclared []string `json:"changedUndeclared,omitempty"`
	HostsUndeclared   []string `json:"hostsUndeclared,omitempty"`
	// Warnings are what the phases decided that an operator may want to undo. A run can
	// succeed with warnings, so this is reported next to the outcome rather than inside it:
	// Succeeded stays true and the warnings say what the success cost.
	Warnings []Warning `json:"warnings,omitempty"`
}

// Warning is one thing a phase decided that an operator may want to know about.
//
// The case this exists for is phase 50 keeping image tarballs it does not own, because the host
// carries the marker file that asks it to. Nothing failed, so there is no error to record, and
// without this the only trace is a log line from the middle of the run.
type Warning struct {
	Phase string `json:"phase"`
	// Host is the address of the host this is about, absent for a warning about the run
	// itself. A redacted receipt carries its alias, the same one the phase ledger uses.
	Host string `json:"host,omitempty"`
	// Path is the file this is about, absolute as the host sees it.
	Path string `json:"path,omitempty"`
	// Reason is why, in the vocabulary the phases log: "kept, unmanaged".
	Reason string `json:"reason,omitempty"`
}

// Dry-run classifications, as a receipt reports them.
const (
	// DryRunSkip means a dry run reports the phase and does not run it.
	DryRunSkip = "skip"
	// DryRunReadOnly means the phase reads hosts and changes nothing, so a dry run runs it as
	// itself.
	DryRunReadOnly = "read-only"
	// DryRunOwnPath means the phase has its own dry-run path.
	DryRunOwnPath = "own-path"
)

// dryRunOf renders a phase's dry-run classification as a stable token.
//
// DryRunBehavior.String is not it: that renders the prose label the phase docs are generated
// from ("runs, reads only"), which is the right thing in a document and the wrong thing in a
// field something writes a comparison against.
func dryRunOf(b phase.DryRunBehavior) string {
	switch b {
	case phase.DryRunReadOnly:
		return DryRunReadOnly
	case phase.DryRunOwnPath:
		return DryRunOwnPath
	default:
		return DryRunSkip
	}
}

// phasesOf flattens a RunResult into the receipt's phase ledger, in the order the run reached
// them within each disposition.
func phasesOf(r phase.RunResult) []Phase {
	var out []Phase
	for _, g := range []struct {
		disposition string
		results     []phase.PhaseResult
	}{
		{
			disposition: DispositionRan,
			results:     r.Ran,
		},
		{
			disposition: DispositionPlanned,
			results:     r.Planned,
		},
		{
			disposition: DispositionSkipped,
			results:     r.Skipped,
		},
	} {
		for _, p := range g.results {
			out = append(out, Phase{
				Title:         p.Title,
				Explanation:   p.Explanation,
				Disposition:   g.disposition,
				DryRun:        dryRunOf(p.DryRun),
				Declared:      p.Declared,
				Changed:       p.Changed,
				HostsDeclared: p.HostsDeclared,
				Hosts:         hostChangesOf(p.Hosts),
				Duration:      durationOf(p.Duration),
			})
		}
	}
	return out
}

// hostChangesOf copies the per-host answers across the package boundary, so the receipt's wire
// format is not whatever pkg/phase's struct happens to look like today.
func hostChangesOf(in []phase.HostChange) []HostChange {
	if len(in) == 0 {
		return nil
	}
	out := make([]HostChange, 0, len(in))
	for _, h := range in {
		c := HostChange{
			Host:    h.Host,
			Changed: h.Changed,
		}
		for _, d := range h.Details {
			c.Details = append(c.Details, HostChangeDetail{
				Path:   d.Path,
				Reason: d.Reason,
				Diff:   d.Diff,
			})
		}
		out = append(out, c)
	}
	return out
}

// durationOf renders a duration as a string rather than as nanoseconds.
//
// A receipt is read by people at least as often as by programs, and "1m13.2s" is legible where
// 73200000000 is not. A zero duration renders empty and is omitted: a phase that never ran did
// not take no time, it took no time because it did not happen.
//
// The rounding changes with the scale so that a fast failure still reports something. Rounding
// everything to the millisecond turns every sub-millisecond run into "0s", which reads as a
// missing measurement rather than a quick one.
func durationOf(d time.Duration) string {
	switch {
	case d == 0:
		return ""
	case d < time.Second:
		return d.Round(time.Microsecond).String()
	default:
		return d.Round(time.Millisecond).String()
	}
}
