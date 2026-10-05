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

// stateInfoPlugin is the plugin under test, read before the playbook runs. See zarfPlaybookEnv for
// why reading it matters.
const stateInfoPlugin = "ansible/colonel_byte/zarf/plugins/action/zarf_state_info.py"

// envStateFixture names the state document the stub zarf serves.
const envStateFixture = "CARGOSHIP_E2E_ZARF_STATE"

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
	env := zarfPlaybookEnv(t, []string{stateInfoPlugin, infoBasePlugin})
	stub := env.executable(t, zarfStub)

	fixture, err := os.ReadFile(filepath.Join(env.root, stateFixture))
	require.NoError(t, err)
	credentials := fixtureCredentials(t, fixture)
	require.NotEmpty(t, credentials, "the state fixture holds no %s value", fixtureCredentialPrefix)

	out := runZarfPlaybook(t, env, stateRedactionPlaybook, []string{
		envZarfStub + "=" + stub,
		envStateFixture + "=" + filepath.Join(env.root, stateFixture),
	}, nil)

	// The credentials must not be in the output of a run that did not ask for them. The playbook
	// asserts that too, but it asserts over the dictionary it was given; this reads the transcript
	// an operator or a CI log would keep. The run with include_credentials is in the same
	// transcript, which is what makes this an assertion about the self-censoring result as well.
	for _, credential := range credentials {
		require.NotContains(t, out, credential,
			"the credential %s reached the playbook's own output", credential)
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
