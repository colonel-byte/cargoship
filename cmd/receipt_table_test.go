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
	"bytes"
	"testing"

	"github.com/colonel-byte/cargoship/internal/receipt"
	"github.com/stretchr/testify/require"
)

// TestChangedCellSeparatesUnaskedFromUnchanged is the distinction the per-host work exists for.
// "no" claims nothing changed; "-" admits nothing was asked. Collapsing them would make the
// table assert something cargoship does not know.
func TestChangedCellSeparatesUnaskedFromUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name string
		host receipt.Host
		want string
	}{
		{
			name: "no phase reports per host at all",
			host: receipt.Host{
				PhasesReporting: 0,
			},
			want: "-",
		},
		{
			name: "phases reported but none named this host",
			host: receipt.Host{
				PhasesNamed:     0,
				PhasesReporting: 3,
			},
			want: "-",
		},
		{
			name: "named and unchanged",
			host: receipt.Host{
				Changed:         false,
				PhasesNamed:     2,
				PhasesReporting: 3,
			},
			want: "no",
		},
		{
			name: "named and changed",
			host: receipt.Host{
				Changed:         true,
				PhasesNamed:     2,
				PhasesReporting: 3,
			},
			want: "yes",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Contains(t, changedCell(tc.host), tc.want)
		})
	}
}

// TestPhasesCellCarriesTheDenominator pins that coverage is always reported as a fraction. A bare
// count cannot be read: two of two is complete and two of fifteen is not.
func TestPhasesCellCarriesTheDenominator(t *testing.T) {
	require.Equal(t, "-", phasesCell(receipt.Host{PhasesReporting: 0}))
	require.Equal(t, "0/4", phasesCell(receipt.Host{PhasesNamed: 0, PhasesReporting: 4}))
	require.Equal(t, "3/4", phasesCell(receipt.Host{PhasesNamed: 3, PhasesReporting: 4}))
}

// TestPrintHostSummaryStaysQuietWithNothingToSay pins that a run where no phase reported per host
// prints no table. A grid of dashes tells an operator less than no grid at all.
func TestPrintHostSummaryStaysQuietWithNothingToSay(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    receipt.Receipt
	}{
		{
			name: "no hosts",
			r:    receipt.Receipt{},
		},
		{
			name: "hosts, but nothing reported per host",
			r: receipt.Receipt{
				Hosts: []receipt.Host{
					{
						Address:         "10.0.0.1",
						PhasesReporting: 0,
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			printHostSummary(&buf, tc.r)
			require.Empty(t, buf.String())
		})
	}
}

// TestPrintHostSummaryRendersEveryHost pins that the table names hosts no phase spoke for, which
// is the host a failed run most needs to show.
func TestPrintHostSummaryRendersEveryHost(t *testing.T) {
	var buf bytes.Buffer
	printHostSummary(&buf, receipt.Receipt{
		Hosts: []receipt.Host{
			{
				Address:         "10.0.0.1",
				Role:            "controller",
				Changed:         true,
				PhasesNamed:     2,
				PhasesReporting: 2,
			},
			{
				Address:         "10.0.0.2",
				Role:            "worker",
				PhasesNamed:     0,
				PhasesReporting: 2,
			},
		},
	})

	out := buf.String()
	require.Contains(t, out, "10.0.0.1")
	require.Contains(t, out, "controller")
	require.Contains(t, out, "2/2")
	require.Contains(t, out, "10.0.0.2")
	require.Contains(t, out, "worker")
	require.Contains(t, out, "0/2")
}

// TestPrintCoverageNoteSaysHowMuchIsCovered pins the hedge. A table read without it understates
// what a run did, because most phases do not report per host.
func TestPrintCoverageNoteSaysHowMuchIsCovered(t *testing.T) {
	var buf bytes.Buffer
	printCoverageNote(&buf, receipt.Receipt{
		Phases: []receipt.Phase{{Title: "a"}, {Title: "b"}, {Title: "c"}},
		Outcome: receipt.Outcome{
			HostsUndeclared: []string{"a", "b"},
		},
	})
	require.Contains(t, buf.String(), "2 of 3 phases do not report per host")

	// Nothing to hedge when every phase spoke.
	var quiet bytes.Buffer
	printCoverageNote(&quiet, receipt.Receipt{Phases: []receipt.Phase{{Title: "a"}}})
	require.Empty(t, quiet.String())
}
