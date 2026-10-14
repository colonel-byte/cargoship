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

package cmd

import (
	"fmt"
	"io"

	"github.com/colonel-byte/cargoship/internal/receipt"
	"github.com/fatih/color"
	"github.com/zarf-dev/zarf/src/pkg/message"
)

// printHostSummary writes the per-host summary of a run.
//
// It is printed from the receipt rather than from the run result because the receipt is where the
// configuration's hosts and the phases' answers have already been reconciled -- a host no phase
// spoke for is in there, and it is the one a failed run most needs to show.
//
// Nothing is printed when no phase reported per host, which is most runs today: a table of four
// hosts and five dashes tells an operator less than no table at all.
func printHostSummary(w io.Writer, r receipt.Receipt) {
	if len(r.Hosts) == 0 || !anyHostReported(r) {
		return
	}

	rows := make([][]string, 0, len(r.Hosts))
	for _, h := range r.Hosts {
		rows = append(rows, []string{
			h.Address,
			h.Role,
			changedCell(h),
			phasesCell(h),
		})
	}

	message.TableWithWriter(w, []string{"Host", "Role", "Changed", "Phases"}, rows)
	printCoverageNote(w, r)
}

// anyHostReported reports whether any phase declared per-host answers at all.
func anyHostReported(r receipt.Receipt) bool {
	for _, h := range r.Hosts {
		if h.PhasesReporting > 0 {
			return true
		}
	}
	return false
}

// changedCell renders what a host did.
//
// A host no phase spoke for gets "-" rather than "no". The distinction is the whole point of the
// per-host work: "no" is a claim that nothing changed, and "-" is the honest admission that
// nothing was asked.
func changedCell(h receipt.Host) string {
	switch {
	case h.PhasesReporting == 0 || h.PhasesNamed == 0:
		return "-"
	case h.Changed:
		return colorWrap("yes", color.FgYellow)
	default:
		return colorWrap("no", color.FgGreen)
	}
}

// phasesCell renders how much of the run spoke for this host, as a fraction.
//
// The denominator is phases that report per host, not phases that ran. A bare count cannot be
// read: named by two of two reporting phases is complete coverage and named by two of fifteen is
// not, and they look identical without it.
func phasesCell(h receipt.Host) string {
	if h.PhasesReporting == 0 {
		return "-"
	}
	return fmt.Sprintf("%d/%d", h.PhasesNamed, h.PhasesReporting)
}

// printCoverageNote says how much of the run the table above speaks for.
//
// Most phases do not report per host, so a table read without this would understate what a run
// did -- a host shown as changing nothing may simply never have been spoken for. This is the same
// partial-versus-complete framing the Ansible module reports as changedSignal, for the same
// reason: a confident number that is quietly wrong is worse than a hedged one.
func printCoverageNote(w io.Writer, r receipt.Receipt) {
	undeclared := len(r.Outcome.HostsUndeclared)
	if undeclared == 0 {
		return
	}

	fmt.Fprintf(w, "%s %d of %d phases do not report per host, so this table covers part of the run.\n",
		colorWrap("note:", color.FgCyan), undeclared, len(r.Phases))
}
