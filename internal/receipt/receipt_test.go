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
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/pkg/phase"
	rigos "github.com/k0sproject/rig/v2/os"
	"github.com/k0sproject/rig/v2/protocol/ssh"
	"github.com/stretchr/testify/require"
)

// hostAt builds a host the way a parsed cluster configuration would, with the connection config
// set and no rig client -- which is the state every host is in by the time a receipt is built,
// since the disconnect phase has already run.
func hostAt(address, role string) *cluster.Host {
	h := &cluster.Host{Role: role}
	h.ConnectionConfig.SSH = &ssh.Config{Address: address}
	return h
}

// TestBuildRecordsEveryConfiguredHost pins that the host list comes from the configuration rather
// than from what the phases reported. A host no phase reached is the one a failed run most needs
// to show, and building from the reports alone would drop it.
func TestBuildRecordsEveryConfiguredHost(t *testing.T) {
	reached := hostAt("10.0.0.1", cluster.RoleController)
	reached.Metadata.Hostname = "kc01"
	reached.Metadata.Arch = "amd64"
	reached.OSRelease = &rigos.Release{ID: "rocky", Version: "9.4"}

	cfg := &cluster.Cluster{
		Spec: cluster.Spec{
			Hosts: cluster.Hosts{
				reached,
				hostAt("10.0.0.2", cluster.RoleWorker),
			},
		},
	}

	result := phase.RunResult{
		Ran: []phase.PhaseResult{
			{
				Title:         "sync",
				HostsDeclared: true,
				Hosts: []phase.HostChange{
					{
						Host:    "10.0.0.1",
						Changed: true,
					},
				},
			},
		},
	}

	got := Build(cfg, result, Options{
		Command:  "apply",
		Observed: true,
	})

	require.Len(t, got.Hosts, 2)
	require.Equal(t, 2, got.Cluster.HostCount)

	require.Equal(t, "10.0.0.1", got.Hosts[0].Address)
	require.Equal(t, "kc01", got.Hosts[0].Hostname)
	require.Equal(t, "rocky", got.Hosts[0].OS)
	require.True(t, got.Hosts[0].Changed)
	require.Equal(t, 1, got.Hosts[0].PhasesNamed)
	require.Equal(t, 1, got.Hosts[0].PhasesReporting)

	// The host nothing spoke for is present and honestly empty, rather than absent.
	require.Equal(t, "10.0.0.2", got.Hosts[1].Address)
	require.False(t, got.Hosts[1].Changed)
	require.Zero(t, got.Hosts[1].PhasesNamed)
}

// TestOutcomeDistinguishesUnknownFromUnchanged is the honesty case. A run that never reached its
// phases knows nothing, and reporting that as "nothing changed" is a claim cargoship cannot make.
func TestOutcomeDistinguishesUnknownFromUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name     string
		result   phase.RunResult
		opts     Options
		changed  bool
		signal   string
		hasError bool
	}{
		{
			name:   "never reached its phases",
			result: phase.RunResult{},
			opts: Options{
				Observed: false,
				Err:      errors.New("unreadable package"),
			},
			changed:  false,
			signal:   phase.SignalUnknown,
			hasError: true,
		},
		{
			name: "every phase declared and none changed",
			result: phase.RunResult{
				Ran: []phase.PhaseResult{
					{
						Title:    "quiet",
						Declared: true,
					},
				},
			},
			opts: Options{
				Observed: true,
			},
			changed: false,
			signal:  phase.SignalComplete,
		},
		{
			name: "a phase changed something",
			result: phase.RunResult{
				Ran: []phase.PhaseResult{
					{
						Title:    "sync",
						Declared: true,
						Changed:  true,
					},
					{
						Title:    "silent",
						Declared: false,
					},
				},
			},
			opts: Options{
				Observed: true,
			},
			changed: true,
			signal:  phase.SignalPartial,
		},
		{
			name: "a dry run reports what a real run would change",
			result: phase.RunResult{
				Planned: []phase.PhaseResult{
					{
						Title:    "would sync",
						Declared: true,
					},
				},
			},
			opts: Options{
				Observed: true,
				DryRun:   true,
			},
			changed: true,
			signal:  phase.SignalComplete,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Build(nil, tc.result, tc.opts)
			require.Equal(t, tc.changed, got.Outcome.Changed)
			require.Equal(t, tc.signal, got.Outcome.ChangedSignal)
			require.Equal(t, !tc.hasError, got.Outcome.Succeeded)
			if !tc.hasError {
				require.Empty(t, got.Outcome.Error)
			} else {
				require.NotEmpty(t, got.Outcome.Error)
			}
		})
	}
}

// TestPhaseLedgerRecordsDisposition pins the three dispositions and the stable dry-run tokens.
// The tokens matter: DryRunBehavior.String renders the prose the phase docs are built from, and
// a receipt field is compared rather than read aloud.
func TestPhaseLedgerRecordsDisposition(t *testing.T) {
	result := phase.RunResult{
		Ran: []phase.PhaseResult{
			{
				Title:    "connect",
				DryRun:   phase.DryRunReadOnly,
				Duration: 1500 * time.Millisecond,
			},
		},
		Planned: []phase.PhaseResult{
			{
				Title:  "install",
				DryRun: phase.DryRunSkip,
			},
		},
		Skipped: []phase.PhaseResult{
			{
				Title:  "sync",
				DryRun: phase.DryRunOwnPath,
			},
		},
	}

	got := Build(nil, result, Options{Observed: true})
	require.Len(t, got.Phases, 3)

	require.Equal(t, DispositionRan, got.Phases[0].Disposition)
	require.Equal(t, DryRunReadOnly, got.Phases[0].DryRun)
	require.Equal(t, "1.5s", got.Phases[0].Duration)

	require.Equal(t, DispositionPlanned, got.Phases[1].Disposition)
	require.Equal(t, DryRunSkip, got.Phases[1].DryRun)
	// A phase that never ran took no time, and reports no duration rather than "0s".
	require.Empty(t, got.Phases[1].Duration)

	require.Equal(t, DispositionSkipped, got.Phases[2].Disposition)
	require.Equal(t, DryRunOwnPath, got.Phases[2].DryRun)
}

// TestRedactArgs covers both spellings a flag value can take.
func TestRedactArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want []string
	}{
		{
			name: "nothing to redact",
			argv: []string{"apply", "./distro.tar.zst", "--config", "cluster.yaml"},
			want: []string{"apply", "./distro.tar.zst", "--config", "cluster.yaml"},
		},
		{
			name: "a path-valued key flag is kept, because a path is not key material",
			argv: []string{"apply", "--vault-password-file", "/etc/cargoship/vault.pass"},
			want: []string{"apply", "--vault-password-file", "/etc/cargoship/vault.pass"},
		},
		{
			name: "separate value",
			argv: []string{"apply", "--registry-password", "hunter2", "--confirm"},
			want: []string{"apply", "--registry-password", "<redacted>", "--confirm"},
		},
		{
			name: "joined value",
			argv: []string{"apply", "--registry-password=hunter2"},
			want: []string{"apply", "--registry-password=<redacted>"},
		},
		{
			name: "a bare value that happens to match a flag name is left alone",
			argv: []string{"apply", "token"},
			want: []string{"apply", "token"},
		},
		{
			name: "empty",
			argv: nil,
			want: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, redactArgs(tc.argv))
		})
	}
}

// TestWriteCreatesAPrivateFile pins the mode, the parent directory, and that a reused path does
// not inherit permissions from whatever was there before.
func TestWriteCreatesAPrivateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "receipt.json")

	require.NoError(t, Write(path, Receipt{SchemaVersion: SchemaVersion}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, fileMode, info.Mode().Perm())

	// A receipt written into a file somebody left world-readable must not stay world-readable.
	require.NoError(t, os.Chmod(path, 0o644))
	require.NoError(t, Write(path, Receipt{SchemaVersion: SchemaVersion}))
	info, err = os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, fileMode, info.Mode().Perm())
}

// TestWriteReadRoundTrip pins that the document survives the wire format.
func TestWriteReadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipt.json")

	want := Build(
		&cluster.Cluster{
			Metadata: cluster.Metadata{Name: "fleet"},
			Spec: cluster.Spec{
				Hosts: cluster.Hosts{hostAt("10.0.0.1", cluster.RoleController)},
			},
		},
		phase.RunResult{
			Ran: []phase.PhaseResult{
				{
					Title:    "connect",
					Declared: true,
				},
			},
		},
		Options{
			Command:   "apply",
			Observed:  true,
			StartedAt: time.Now().Add(-time.Minute),
		},
	)

	require.NoError(t, Write(path, want))
	got, err := Read(path)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// TestHostFieldsAreAnAllowlist is the test that has to keep working as the type grows.
//
// The receipt must never carry a host fact nobody classified, because cluster.Host holds
// connection state and credentials reachable through the embedded rig client. Pinning the field
// set means adding one to Host fails here until somebody decides, deliberately, that it belongs
// in an artifact that gets attached to tickets.
func TestHostFieldsAreAnAllowlist(t *testing.T) {
	want := []string{
		"Address",
		"Hostname",
		"Role",
		"Profile",
		"Arch",
		"OS",
		"OSVersion",
		"PrivateAddress",
		"PrivateInterface",
		"EngineVersion",
		"Changed",
		"PhasesNamed",
		"PhasesReporting",
	}

	require.Equal(t, want, fieldNames[Host](),
		"a field was added to or removed from Host; confirm it is safe to record before updating this list")
}

// fieldNames lists the declared fields of a struct type, in declaration order. It backs the
// allowlist tests, which pin field sets so that adding one has to be a decision rather than an
// accident.
func fieldNames[T any]() []string {
	typ := reflect.TypeOf(*new(T))
	out := make([]string, 0, typ.NumField())
	for i := range typ.NumField() {
		out = append(out, typ.Field(i).Name)
	}
	return out
}
