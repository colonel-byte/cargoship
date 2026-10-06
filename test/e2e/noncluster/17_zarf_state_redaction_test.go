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

package noncluster

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The playbook that holds the redaction, the state it is served, and the zarf that serves it.
const (
	stateRedactionPlaybook = "test/e2e/noncluster/testdata/zarf-state-redaction.yml"
	stateFixture           = "test/e2e/noncluster/testdata/zarf-state.json"
	zarfStub               = "test/e2e/noncluster/testdata/zarf-state-stub.sh"
)

// stateInfoPlugin is the plugin under test. This test reads it without parsing it, and that is the
// whole reason: Go's test cache keys on the files the test process opens, and everything this test
// actually exercises is opened by the ansible-playbook child instead. Without reading them here, an
// edit to the plugin or the playbook leaves a passing result cached -- which is exactly the shape of
// mistake that gets a credential leak marked as fixed.
const stateInfoPlugin = "ansible/colonel_byte/zarf/plugins/action/zarf_state_info.py"

// The environment the stub and the playbook agree on: which state to serve, and where the stub is.
const (
	envStateFixture = "CARGOSHIP_E2E_ZARF_STATE"
	envZarfStub     = "CARGOSHIP_E2E_ZARF_STUB"
)

// assertedPrefix is what every assert task in the playbook says when it passes. Counting them is
// what catches a playbook whose tasks stopped running -- a renamed fact or a skipped block leaves
// every remaining assertion passing, and `failed=0` alone would call that a success. How many to
// expect is read out of the playbook rather than written here, so that adding an assertion to the
// playbook does not quietly stop the count from meaning anything.
const assertedPrefix = "ASSERTED "

// fixtureCredentialPrefix marks every credential value in the state fixture, so that this test can
// ask the fixture what to look for instead of keeping a second copy of the list.
const fixtureCredentialPrefix = "FIXTURE-"

// TestZarfStateInfoWithholdsCredentials holds what zarf_state_info returns against what the
// zarf-state Secret carries.
//
// The Secret holds eight credentials -- the registry push, pull and seed secrets, the git server
// push and pull passwords, the artifact server token, and the agent webhook's TLS private key --
// and the module removes them unless the operator asks for them. That is a contract about the
// contents of a returned dictionary, so unlike the flag rendering in
// internal/zarfmod/infoplugins_test.go it cannot be read out of the source: it has to be run.
//
// It is here rather than in test/e2e/zarf because it needs no cluster. The module's one impure step
// is running zarf, so a stub zarf that prints the base64 of a recorded state exercises everything
// else -- the decode, the redaction, the self-censoring result, and the role that wraps it. See
// docs/agent/choice-zarf-info-modules.md.
func TestZarfStateInfoWithholdsCredentials(t *testing.T) {
	ansible, err := exec.LookPath("ansible-playbook")
	if err != nil {
		t.Skip("ansible-playbook is not on PATH: the redaction lives in an action plugin, which " +
			"only Ansible can run")
	}

	root, err := os.Getwd()
	require.NoError(t, err)

	// The layout Ansible insists on and the repository does not have: a collections path holding
	// an ansible_collections directory, whose children are the namespaces.
	collections := t.TempDir()
	require.NoError(t, os.Symlink(
		filepath.Join(root, "ansible"),
		filepath.Join(collections, "ansible_collections"),
	))

	// Read before running, so the cache sees them. See stateInfoPlugin above.
	playbook, err := os.ReadFile(filepath.Join(root, stateRedactionPlaybook))
	require.NoError(t, err)
	fixture, err := os.ReadFile(filepath.Join(root, stateFixture))
	require.NoError(t, err)
	_, err = os.ReadFile(filepath.Join(root, stateInfoPlugin))
	require.NoError(t, err)

	expectedAssertions := strings.Count(string(playbook), "success_msg: "+assertedPrefix)
	require.NotZero(t, expectedAssertions, "the playbook holds no assertions")

	credentials := fixtureCredentials(t, fixture)
	require.NotEmpty(t, credentials, "the state fixture holds no %s value", fixtureCredentialPrefix)

	stub := filepath.Join(root, zarfStub)
	info, err := os.Stat(stub)
	require.NoError(t, err, "the stub zarf is missing")
	require.NotZero(t, info.Mode()&0o111, "the stub zarf is not executable")

	cmd := exec.Command(ansible, //nolint:gosec // the path is from LookPath and this test's constants
		"-i", "localhost,",
		filepath.Join(root, stateRedactionPlaybook),
	)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"ANSIBLE_COLLECTIONS_PATH="+collections,
		envStateFixture+"="+filepath.Join(root, stateFixture),
		envZarfStub+"="+stub,
		// The playbook asserts on what the module returned, and a localhost fact gather is both
		// slow and irrelevant to that.
		"ANSIBLE_GATHERING=explicit",
	)

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "the playbook failed:\n%s", out)

	asserted := strings.Count(string(out), assertedPrefix)
	require.Equal(t, expectedAssertions, asserted,
		"%d of the playbook's %d assert tasks reported passing, so some of them did not run:\n%s",
		asserted, expectedAssertions, out)

	// The credentials must not be in the output of a run that did not ask for them. The playbook
	// asserts that too, but it asserts over the dictionary it was given; this reads the transcript
	// an operator or a CI log would keep. The run with include_credentials is in the same
	// transcript, which is what makes this an assertion about the self-censoring result as well.
	for _, credential := range credentials {
		require.NotContains(t, string(out), credential,
			"the credential at %s reached the playbook's own output", credential)
	}
}

// fixtureCredentials returns every credential value in the state fixture, by the prefix they all
// carry. Reading them out of the fixture is what keeps this test and the playbook from disagreeing
// about which values are secret.
func fixtureCredentials(t *testing.T, fixture []byte) []string {
	t.Helper()

	var state map[string]any
	require.NoError(t, json.Unmarshal(fixture, &state))

	var found []string
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			for _, below := range value {
				walk(below)
			}
		case []any:
			for _, below := range value {
				walk(below)
			}
		case string:
			// The agent's TLS key is a Go []byte, so zarf writes it base64-encoded. Its placeholder
			// carries the prefix underneath that encoding rather than in the document, and the
			// encoded form is what a leak would put in a log.
			if decoded, err := base64.StdEncoding.DecodeString(value); err == nil &&
				strings.HasPrefix(string(decoded), fixtureCredentialPrefix) {
				found = append(found, value)
				return
			}
			if strings.HasPrefix(value, fixtureCredentialPrefix) {
				found = append(found, value)
			}
		}
	}
	walk(state)
	return found
}
