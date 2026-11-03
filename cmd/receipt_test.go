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
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/internal/receipt"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/colonel-byte/cargoship/types"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// TestStartReceiptWithoutAPathDoesNothing pins that the finisher is always safe to defer. The
// call sites have no conditional around it, which is the point: forgetting one is only ever
// discovered by the person who needed the receipt.
func TestStartReceiptWithoutAPathDoesNothing(t *testing.T) {
	cmd := &cobra.Command{Use: "apply"}
	ctx, finish, err := startReceipt(context.Background(), cmd, InstallCommon{})

	require.NoError(t, err)
	require.NotNil(t, finish)
	require.NotPanics(t, func() { finish(nil, "", nil, errors.New("boom")) })

	// With no receipt asked for there is no sink, so nothing pays for the feature.
	_, sink := phase.WithResultSink(ctx)
	require.False(t, sink.Observed())
}

// TestStartReceiptWritesOnFailure is the behaviour the whole feature turns on: a failed run is
// the one an auditor most wants a record of, and it is the run whose log lines are least likely
// to still be in a scrollback buffer by the time anyone asks.
func TestStartReceiptWritesOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "apply.json")

	cmd := &cobra.Command{Use: "apply"}
	opt := InstallCommon{
		config:      "/etc/cargoship/cluster.yaml",
		receiptPath: path,
	}

	_, finish, err := startReceipt(context.Background(), cmd, opt)
	require.NoError(t, err)
	// A nil manager is the state a run that failed in preflight is in.
	finish(nil, "", nil, errors.New("unable to read the cluster configuration"))

	got, err := receipt.Read(path)
	require.NoError(t, err)

	require.Equal(t, receipt.SchemaVersion, got.SchemaVersion)
	require.Equal(t, "apply", got.Invocation.Command)
	require.False(t, got.Outcome.Succeeded)
	require.Equal(t, "unable to read the cluster configuration", got.Outcome.Error)

	// Nothing ran, so the answer is unknown rather than "nothing changed".
	require.Equal(t, phase.SignalUnknown, got.Outcome.ChangedSignal)
	require.False(t, got.Outcome.Changed)
	require.Nil(t, got.Package)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestStartReceiptRecordsTheFleet covers the path where a manager exists, which is every run that
// got as far as loading its configuration.
func TestStartReceiptRecordsTheFleet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reset.json")

	host := &cluster.Host{Role: cluster.RoleController}
	manager := &phase.Manager{
		Config: &cluster.Cluster{
			Metadata: cluster.Metadata{Name: "fleet"},
			Spec: cluster.Spec{
				Config: cluster.Config{LoadBalancer: "lb.example.test"},
				Hosts:  cluster.Hosts{host},
			},
		},
	}

	cmd := &cobra.Command{Use: "reset"}
	_, finish, err := startReceipt(context.Background(), cmd, InstallCommon{receiptPath: path})
	require.NoError(t, err)
	finish(manager, "", nil, nil)

	got, err := receipt.Read(path)
	require.NoError(t, err)

	require.Equal(t, "fleet", got.Cluster.Name)
	require.Equal(t, "lb.example.test", got.Cluster.LoadBalancer)
	require.Equal(t, 1, got.Cluster.HostCount)
	require.Len(t, got.Hosts, 1)
	require.Equal(t, cluster.RoleController, got.Hosts[0].Role)

	// Never null, so a consumer can iterate without a nil check.
	require.NotNil(t, got.Phases)
	require.NotNil(t, got.Hosts)
}

// TestPackageProvenanceWithoutALayout pins that a command loading no package -- reset, which
// builds its manager by hand -- produces a receipt with no package block rather than an empty
// one that reads as a package with nothing known about it.
func TestPackageProvenanceWithoutALayout(t *testing.T) {
	require.Nil(t, packageProvenance("", nil))
	require.Nil(t, packageProvenance("/some/package.tar.zst", nil))
}

// TestArchiveDigest pins that only a real file is hashed. A package named by an OCI reference has
// no archive, and an absent digest is the honest answer rather than a digest of something else.
func TestArchiveDigest(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "package.tar.zst")
	require.NoError(t, os.WriteFile(file, []byte("cargoship"), 0o600))

	for _, tc := range []struct {
		name  string
		path  string
		empty bool
	}{
		{
			name:  "a real file is hashed",
			path:  file,
			empty: false,
		},
		{
			name:  "a directory is not",
			path:  dir,
			empty: true,
		},
		{
			name:  "a missing path is not",
			path:  filepath.Join(dir, "absent.tar.zst"),
			empty: true,
		},
		{
			name:  "an OCI reference is not",
			path:  "oci://example.test/distro:1.0.0",
			empty: true,
		},
		{
			name:  "an empty path is not",
			path:  "",
			empty: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := archiveDigest(tc.path)
			if tc.empty {
				require.Empty(t, got)
				return
			}
			require.Len(t, got, 64)
		})
	}
}

// TestReceiptFlagIsRegisteredOnTheLifecycleCommands pins which commands carry the flag, and that
// kube-config does not: it reads a kubeconfig off a controller and changes no host, so there is
// nothing to attest.
func TestReceiptFlagIsRegisteredOnTheLifecycleCommands(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cmd      *cobra.Command
		expected bool
	}{
		{
			name:     "apply",
			cmd:      newInstallApplyCommand(),
			expected: true,
		},
		{
			name:     "prepare",
			cmd:      newInstallPrepareCommand(),
			expected: true,
		},
		{
			name:     "reset",
			cmd:      newInstallResetCommand(),
			expected: true,
		},
		{
			name:     "engine-config-sync",
			cmd:      newInstallEngineConfigSyncCommand(),
			expected: true,
		},
		{
			name:     "kube-config",
			cmd:      newInstallKubeConfigCommand(),
			expected: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.cmd.Flags().Lookup(InstallReceiptPath) != nil)
		})
	}
}

// TestExpandReceiptPath covers the templating that makes `.distro.receipt_path` usable as a
// standing configuration rather than a path every run truncates.
func TestExpandReceiptPath(t *testing.T) {
	started := time.Date(2026, 9, 28, 14, 30, 0, 0, time.UTC)

	for _, tc := range []struct {
		name    string
		path    string
		command string
		want    string
		wantErr string
	}{
		{
			name: "empty stays empty",
			path: "",
			want: "",
		},
		{
			name:    "a literal path is left alone",
			path:    "/var/log/cargoship/apply.json",
			command: "apply",
			want:    "/var/log/cargoship/apply.json",
		},
		{
			name:    "command and timestamp",
			path:    "/var/log/cargoship/receipts/{command}-{timestamp}.json",
			command: "apply",
			want:    "/var/log/cargoship/receipts/apply-20260928-143000.json",
		},
		{
			// Write creates the parent directory, so a placeholder in a directory
			// component works the same as one in the filename.
			name:    "a placeholder in a directory component",
			path:    "/var/log/cargoship/{command}/{timestamp}.json",
			command: "engine-config-sync",
			want:    "/var/log/cargoship/engine-config-sync/20260928-143000.json",
		},
		{
			name:    "repeated placeholders",
			path:    "/receipts/{command}/{command}-{timestamp}.json",
			command: "reset",
			want:    "/receipts/reset/reset-20260928-143000.json",
		},
		{
			name:    "an unknown placeholder is an error",
			path:    "/receipts/{date}.json",
			command: "apply",
			wantErr: "unknown placeholder {date}",
		},
		{
			// "phase" means a numbered step inside a run, not the command that ran, so
			// it is deliberately not an alias for {command}.
			name:    "phase is not an alias for command",
			path:    "/receipts/{phase}.json",
			command: "apply",
			wantErr: "unknown placeholder {phase}",
		},
		{
			name:    "every unknown placeholder is named at once",
			path:    "/receipts/{date}/{cluster}.json",
			command: "apply",
			wantErr: "unknown placeholder {date}, {cluster}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := expandReceiptPath(tc.path, tc.command, started)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestStartReceiptRejectsABadPathBeforeTheRun pins that the failure comes back from startReceipt
// rather than from the finisher. The call sites return it before touching a host, which is the
// whole point: a typo in a receipt path must not cost a fleet-wide run its record.
func TestStartReceiptRejectsABadPathBeforeTheRun(t *testing.T) {
	cmd := &cobra.Command{Use: "apply"}
	_, finish, err := startReceipt(context.Background(), cmd, InstallCommon{receiptPath: "/receipts/{nope}.json"})

	require.ErrorContains(t, err, "unknown placeholder {nope}")
	require.Nil(t, finish)
}

// TestStartReceiptExpandsTheConfiguredPath is the end of the wiring: a templated path reaches the
// file that actually gets written.
func TestStartReceiptExpandsTheConfiguredPath(t *testing.T) {
	dir := t.TempDir()

	cmd := &cobra.Command{Use: "prepare"}
	_, finish, err := startReceipt(context.Background(), cmd, InstallCommon{
		receiptPath: filepath.Join(dir, "{command}-{timestamp}.json"),
	})
	require.NoError(t, err)
	finish(nil, "", nil, nil)

	entries, err := filepath.Glob(filepath.Join(dir, "prepare-*.json"))
	require.NoError(t, err)
	require.Len(t, entries, 1)

	got, err := receipt.Read(entries[0])
	require.NoError(t, err)
	require.Equal(t, "prepare", got.Invocation.Command)
}

// TestReceiptFlagDefaultsToTheConfiguredPath pins the config-to-flag seam: the flag's default is
// the configured value, untemplated, so `--help` shows the operator what they configured and
// expansion still happens once per run.
func TestReceiptFlagDefaultsToTheConfiguredPath(t *testing.T) {
	configured := "/var/log/cargoship/receipts/{command}-{timestamp}.json"

	original := resolvedConfig.DistroOpts.ReceiptPath
	resolvedConfig.DistroOpts.ReceiptPath = configured
	t.Cleanup(func() { resolvedConfig.DistroOpts.ReceiptPath = original })

	cmd := &cobra.Command{Use: "apply"}
	var path string
	registerFlagReceiptPath(cmd, &path)

	require.Equal(t, configured, path)
	require.Equal(t, configured, cmd.Flags().Lookup(InstallReceiptPath).DefValue)
}

// TestConfiguredReceiptPathExpandsHomeThenPlaceholders walks the two rewrites a config-file value
// goes through, in the order initViper applies them. They are far apart in the code -- one in
// expandHomePaths at startup, one in startReceipt per run -- and the composition is what an
// operator actually writes, so it is worth pinning that neither step disturbs the other's syntax.
func TestConfiguredReceiptPathExpandsHomeThenPlaceholders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := types.DistroConfig{
		DistroOpts: types.DistroOptions{
			ReceiptPath: "~/.cache/cargoship/logs/{command}-{timestamp}.json",
		},
	}
	require.NoError(t, expandHomePaths(&cfg))

	got, err := expandReceiptPath(cfg.DistroOpts.ReceiptPath, "apply", time.Date(2026, 9, 28, 14, 30, 0, 0, time.UTC))
	require.NoError(t, err)

	require.Equal(t, filepath.Join(home, ".cache", "cargoship", "logs", "apply-20260928-143000.json"), got)
	require.True(t, filepath.IsAbs(got))
	require.NotContains(t, got, "~")
	require.NotContains(t, got, "{")
}
