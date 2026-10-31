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

import "fmt"

// hostAlias is the name a redacted receipt gives a host, numbered from one in the order the
// receipt lists them.
//
// A sequential counter is used rather than a hash of the real address on purpose. Hostnames and
// RFC-1918 addresses come from a small enough space to enumerate, so a digest of one is
// recoverable by anybody willing to spend an afternoon on it, and would carry the false
// reassurance of looking like a one-way function. A counter carries no information at all.
func hostAlias(i int) string {
	return fmt.Sprintf("host-%02d", i+1)
}

// Redact returns a copy of r that is safe to attach to a public issue.
//
// It is a projection, not an edit: the full receipt stays as it is on disk, because redaction is
// lossy and one-way and the operator's own copy is the audit record. What survives is everything
// a maintainer triages from -- the phase ledger with its dispositions, durations and change
// reasons, the package provenance, the outcome and the error -- and what goes is everything that
// identifies the fleet or could carry a secret:
//
//   - Hosts become host-01, host-02 in the order the receipt lists them, consistently across
//     every phase, so a reader can still follow which host failed without learning its address.
//   - Diff bodies are dropped. This is the sharpest edge: an engine config.yaml diff carries
//     join tokens, TLS SANs and registry credentials. The file's path and the reason it changed
//     are what a maintainer needs; the content is what leaks.
//   - The cluster name, the operator, the management host and the command line are dropped
//     outright. Nothing in triage needs them, and config and package paths routinely carry
//     customer and site names.
func Redact(r Receipt) Receipt {
	out := Receipt{
		SchemaVersion: r.SchemaVersion,
		Redacted:      true,
		Invocation: Invocation{
			// Kept: these say which cargoship ran and how, and identify nobody.
			Command:    r.Invocation.Command,
			Version:    r.Invocation.Version,
			Commit:     r.Invocation.Commit,
			Platform:   r.Invocation.Platform,
			DryRun:     r.Invocation.DryRun,
			StartedAt:  r.Invocation.StartedAt,
			FinishedAt: r.Invocation.FinishedAt,
			Duration:   r.Invocation.Duration,
		},
		Cluster: Cluster{
			// The host count is the shape of the fleet, which matters for reproducing a bug.
			// The name, the load balancer and the config path are not.
			HostCount: r.Cluster.HostCount,
		},
		// Outcome is carried whole except for the warnings, whose host addresses are aliased
		// below with every other address in the receipt.
		Outcome: outcomeWithoutWarnings(r.Outcome),
	}

	if r.Package != nil {
		out.Package = &Package{
			// The path is dropped and the digests kept: a digest identifies the package
			// without saying anything about where it came from or who for.
			Name:         r.Package.Name,
			Version:      r.Package.Version,
			SHA256:       r.Package.SHA256,
			Digest:       r.Package.Digest,
			Architecture: r.Package.Architecture,
			Engine:       r.Package.Engine,
			Signed:       r.Package.Signed,
			Verification: r.Package.Verification,
		}
	}

	aliases := hostAliases(r)

	out.Hosts = make([]Host, 0, len(r.Hosts))
	for _, h := range r.Hosts {
		out.Hosts = append(out.Hosts, Host{
			Address: aliases[h.Address],
			// Hostname, the private address and the private interface are dropped: each one
			// names the fleet as surely as the address does. The role, architecture and OS
			// stay, because a bug that only happens on a Rocky 9 worker is a different bug.
			Role:            h.Role,
			Profile:         h.Profile,
			Arch:            h.Arch,
			OS:              h.OS,
			OSVersion:       h.OSVersion,
			EngineVersion:   h.EngineVersion,
			Changed:         h.Changed,
			PhasesNamed:     h.PhasesNamed,
			PhasesReporting: h.PhasesReporting,
		})
	}

	for _, w := range r.Outcome.Warnings {
		out.Outcome.Warnings = append(out.Outcome.Warnings, Warning{
			Phase: w.Phase,
			Host:  aliases[w.Host],
			// The path and the reason are what a maintainer triages from, the same way they
			// are kept in a phase's change details.
			Path:   w.Path,
			Reason: w.Reason,
		})
	}

	out.Phases = make([]Phase, 0, len(r.Phases))
	for _, p := range r.Phases {
		redacted := Phase{
			Title:         p.Title,
			Explanation:   p.Explanation,
			Disposition:   p.Disposition,
			DryRun:        p.DryRun,
			Declared:      p.Declared,
			Changed:       p.Changed,
			HostsDeclared: p.HostsDeclared,
			Duration:      p.Duration,
		}
		for _, h := range p.Hosts {
			change := HostChange{
				Host:    aliases[h.Host],
				Changed: h.Changed,
			}
			for _, d := range h.Details {
				change.Details = append(change.Details, HostChangeDetail{
					Path:   d.Path,
					Reason: d.Reason,
					// Diff is deliberately not carried over.
				})
			}
			redacted.Hosts = append(redacted.Hosts, change)
		}
		out.Phases = append(out.Phases, redacted)
	}

	return out
}

// hostAliases maps every address the receipt mentions to its alias.
//
// The host list is walked first so the numbering follows the cluster configuration rather than
// the order the phases happened to reach hosts in. An address that appears only inside a phase --
// a host added to the fleet by something other than the configuration, or a receipt assembled by
// an older version -- still gets an alias, because an unmapped address would otherwise fall
// through as the empty string and silently merge two hosts into one.
func hostAliases(r Receipt) map[string]string {
	aliases := map[string]string{}
	next := 0

	assign := func(address string) {
		if address == "" {
			return
		}
		if _, ok := aliases[address]; ok {
			return
		}
		aliases[address] = hostAlias(next)
		next++
	}

	for _, h := range r.Hosts {
		assign(h.Address)
	}
	for _, p := range r.Phases {
		for _, h := range p.Hosts {
			assign(h.Host)
		}
	}
	// A host named only by a warning still gets an alias, for the same reason a host named only
	// by a phase does: an unmapped address falls through as the empty string.
	for _, w := range r.Outcome.Warnings {
		assign(w.Host)
	}
	return aliases
}

// outcomeWithoutWarnings copies an outcome, leaving the warnings for the caller to carry over
// once the host aliases exist. Spelled out rather than mutating a copy so that a field added to
// Outcome is carried by default and TestRedactedFieldsAreClassified is what catches the
// exceptions.
func outcomeWithoutWarnings(o Outcome) Outcome {
	o.Warnings = nil
	return o
}
