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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	goyaml "github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

const resolvedInventory = `{
  "groups": {
    "all": ["kc01", "kc02", "kw01", "db01"],
    "controller": ["kc01", "kc02"],
    "worker": ["kw01"],
    "databases": ["db01"]
  },
  "hostvars": {
    "kc01": {"ansible_host": "10.1.2.3", "cargoship_hostname": "distro-kc01"},
    "kc02": {"ansible_host": "10.1.2.4", "cargoship_hostname": "distro-kc02"},
    "kw01": {"ansible_host": "10.1.2.5", "cargoship_hostname": "distro-kw01"},
    "db01": {"ansible_host": "10.1.2.9"}
  },
  "cluster": {"name": "bubbles", "loadbalancer": "bubbles-kc.test.com"}
}`

// runFromAnsible builds the command the way the root does and runs it, returning stdout and
// stderr separately: the inventory goes to one and everything else to the other, and that
// separation is what makes the output redirectable into a file or a validate run.
func runFromAnsible(t *testing.T, stdin string, args ...string) (stdout string, stderr string, err error) {
	t.Helper()

	f := &keyFlags{}
	cmd := newInventoryFromAnsibleCommand(f)
	cmd.Flags().StringVar(&f.vaultPasswordFile, MiscVaultPasswordFile, "", "")
	addAgeFlags(cmd, f)
	// The root silences usage on error, and these tests run the subcommand without it. Without
	// this the usage text lands on stdout and the check that a failed run writes no document
	// would be testing cobra rather than the command.
	cmd.SilenceUsage = true
	var out, errOut bytes.Buffer
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func decodeInventory(t *testing.T, doc string) *cluster.ZarfCluster {
	t.Helper()
	var out cluster.ZarfCluster
	require.NoError(t, goyaml.Unmarshal([]byte(doc), &out))
	return &out
}

func TestInventoryFromAnsibleEncryptsWithAge(t *testing.T) {
	pubKey := "age1wjqegc62gpyvp4yfdqfk4vclfgdh3awlv03rgthcje398a860p7qpglp6w"
	inv := `{
		"groups": {"controller": ["kc01"]},
		"cluster": {
			"name": "bubbles",
			"loadbalancer": "bubbles-kc.test.com",
			"registries": [{
				"name": "docker.io",
				"auth": {"user": "alice", "pass": "secret"}
			}]
		}
	}`

	stdout, stderr, err := runFromAnsible(t, inv, "--age-recipient", pubKey)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "-----BEGIN AGE ENCRYPTED FILE-----")
}

func TestInventoryFromAnsibleWritesToStdout(t *testing.T) {
	stdout, stderr, err := runFromAnsible(t, resolvedInventory)
	require.NoError(t, err)
	require.Empty(t, stderr)

	out := decodeInventory(t, stdout)
	require.Equal(t, "bubbles", out.Metadata.Name)
	require.Equal(t, "bubbles-kc.test.com", out.Spec.Config.LoadBalancer)
	require.Len(t, out.Spec.Hosts, 3, "a host in no mapped group is not part of the cluster")

	// Controllers come first, because the first host of the document becomes the leader.
	require.Equal(t, cluster.RoleController, out.Spec.Hosts[0].Role)
	require.Equal(t, "distro-kc01", out.Spec.Hosts[0].Hostname)
	require.Equal(t, cluster.RoleWorker, out.Spec.Hosts[2].Role)
}

func TestInventoryFromAnsibleReadsAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolved.json")
	require.NoError(t, os.WriteFile(path, []byte(resolvedInventory), 0o600))

	stdout, _, err := runFromAnsible(t, "", path)
	require.NoError(t, err)
	require.Equal(t, "bubbles", decodeInventory(t, stdout).Metadata.Name)
}

func TestInventoryFromAnsibleWritesAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.yaml")

	stdout, stderr, err := runFromAnsible(t, resolvedInventory, "-o", path)
	require.NoError(t, err)
	require.Empty(t, stdout, "the note about the written file belongs on stderr")
	require.Contains(t, stderr, path)

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "bubbles", decodeInventory(t, string(b)).Metadata.Name)

	// The document carries connection details, so it is not written world-readable.
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestInventoryFromAnsibleFlagsOverrideTheInput(t *testing.T) {
	stdout, _, err := runFromAnsible(t, resolvedInventory,
		"--name", "staging", "--loadbalancer", "staging-kc.test.com")
	require.NoError(t, err)

	out := decodeInventory(t, stdout)
	require.Equal(t, "staging", out.Metadata.Name)
	require.Equal(t, "staging-kc.test.com", out.Spec.Config.LoadBalancer)
}

func TestInventoryFromAnsibleErrors(t *testing.T) {
	tests := []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{
			name:  "unknown top level key",
			stdin: `{"groups": {"controller": ["kc01"]}, "clustr": {"name": "bubbles"}}`,
			want:  `unknown field "clustr"`,
		},
		{
			name:  "not JSON at all",
			stdin: "groups:\n  controller:\n    - kc01\n",
			want:  "unable to read the Ansible inventory",
		},
		{
			name:  "no cluster name",
			stdin: `{"groups": {"controller": ["kc01"]}, "cluster": {"loadbalancer": "kc.test.com"}}`,
			want:  "cluster name is required",
		},
		{
			name:  "host takes two roles",
			stdin: `{"groups": {"controller": ["kc01"], "worker": ["kc01"]}, "cluster": {"name": "bubbles", "loadbalancer": "kc.test.com"}}`,
			want:  "a host takes one role",
		},
		{
			name: "missing file",
			args: []string{filepath.Join(t.TempDir(), "absent.json")},
			want: "unable to read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, _, err := runFromAnsible(t, tt.stdin, tt.args...)
			require.ErrorContains(t, err, tt.want)
			require.Empty(t, stdout, "a failed translation writes no document")
		})
	}
}
