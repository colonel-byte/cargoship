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

package zarf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assertedPrefix is what every assert task in info-modules.yml says when it passes. The playbook
// holds the assertions about the data, because they are about a returned dictionary and reading one
// back into Go would be a second copy of the same expectations; what is held here is that every one
// of those tasks ran. A play that stopped early, or a renamed fact that skipped a block, leaves the
// remaining assertions passing and the playbook exiting 0.
const assertedPrefix = "ASSERTED "

// deployedPackages are the packages a completed walk leaves on the cluster. The init package is
// tracked under "init" rather than under its own metadata name.
var deployedPackages = []string{ //nolint:gochecknoglobals
	"init",
	"csi-local-path-provider",
}

// testInfoModules runs the read-only modules and their roles against a cluster a walk has
// initialised.
//
// It is not a test of its own because it needs what a walk produced: `zarf package list` answers
// with the packages the walk deployed, and the zarf-state Secret exists because `zarf init` wrote
// it. A cluster of its own would cost ten minutes to tell it nothing is deployed.
//
// What it adds over the fixture-based test in test/e2e/noncluster is the state zarf actually
// writes. That test holds the redaction against a recorded document, which cannot notice a zarf
// release that adds a credential under a name nobody has read; this one reads the real Secret, so
// the field set it checks is zarf's.
func testInfoModules(t *testing.T, cluster *cluster) {
	t.Helper()

	out, results := cluster.walk(t, infoModulesPlaybook)

	expected := countAssertions(t, infoModulesPlaybook)
	if got := strings.Count(out, assertedPrefix); got != expected {
		t.Errorf("%d of the playbook's %d assert tasks reported passing, so some of them did not run",
			got, expected)
	}

	// The playbook asserted over the dictionaries it was given. These read what it recorded, which
	// is the same path a developer takes when one of them fails.
	packages := readStep(t, results, "info-packages.json")
	assertListedPackages(t, packages)

	state := readStep(t, results, "info-state.json")
	assertStateRecordIsRedacted(t, state, out)
}

// countAssertions is how many assert tasks the playbook holds, read out of the playbook rather than
// written here, so that adding one does not quietly stop the count from meaning anything.
func countAssertions(t *testing.T, playbook string) int {
	t.Helper()

	source, err := os.ReadFile(filepath.Join(suite.root, playbook)) //nolint:gosec // the suite's own constant
	if err != nil {
		t.Fatalf("unable to read %s: %v", playbook, err)
	}
	count := strings.Count(string(source), "success_msg: "+assertedPrefix)
	if count == 0 {
		t.Fatalf("%s holds no assertions", playbook)
	}
	return count
}

// assertListedPackages holds the recorded package list against the packages a walk deploys.
func assertListedPackages(t *testing.T, result map[string]any) {
	t.Helper()

	if changed, ok := result["changed"].(bool); !ok || changed {
		t.Errorf("zarf_package_info reported changed %v; reading a cluster changes nothing", result["changed"])
	}

	listed, ok := result["packages"].([]any)
	if !ok {
		t.Fatalf("the result carries no packages list: %v", result["packages"])
	}

	found := map[string]bool{}
	for _, entry := range listed {
		record, ok := entry.(map[string]any)
		if !ok {
			t.Errorf("a package record is not an object: %v", entry)
			continue
		}
		name, ok := record["package"].(string)
		if !ok {
			t.Errorf("a package record carries no name: %v", record)
			continue
		}
		found[name] = true
		if version, ok := record["version"].(string); !ok || version == "" {
			t.Errorf("the package %s was listed with no version", name)
		}
	}

	for _, name := range deployedPackages {
		if !found[name] {
			t.Errorf("the walk deployed %s, which the package list does not name", name)
		}
		delete(found, name)
	}
	for name := range found {
		t.Errorf("the package list names %s, which no walk deployed", name)
	}
}

// assertStateRecordIsRedacted holds what was written to disk, and what reached the transcript.
//
// The playbook records the redacted result rather than the full one, so this is also the assertion
// that it recorded the right one: a credential in a file under the suite's temporary directory, or
// in the output a failure is diagnosed from, is a credential that escaped.
func assertStateRecordIsRedacted(t *testing.T, result map[string]any, out string) {
	t.Helper()

	redacted, ok := result["redacted"].([]any)
	if !ok {
		t.Fatalf("the result carries no redacted list: %v", result["redacted"])
	}
	if len(redacted) == 0 {
		t.Error("the recorded state withheld nothing, so either zarf stopped writing credentials " +
			"into the zarf-state Secret or the module stopped withholding them")
	}

	state, ok := result["state"].(map[string]any)
	if !ok {
		t.Fatalf("the result carries no state: %v", result["state"])
	}

	// Walk what was recorded and fail on anything still sitting under a credential's name. The
	// module decides that by leaf name, and so does this: a second copy of the path list would be
	// a second thing to keep in step with zarf.
	var walk func(node any, path string)
	walk = func(node any, path string) {
		switch value := node.(type) {
		case map[string]any:
			for name, below := range value {
				at := name
				if path != "" {
					at = path + "." + name
				}
				if isCredentialName(name) {
					t.Errorf("the recorded state still holds %s", at)
					continue
				}
				walk(below, at)
			}
		case []any:
			for _, below := range value {
				walk(below, path)
			}
		}
	}
	walk(state, "")

	// The full-state run is in the same transcript, and it set _ansible_no_log so that its result
	// was censored. This is what says the censoring held.
	if strings.Contains(out, "pushPassword") {
		t.Error("the playbook transcript names pushPassword, so a credential was displayed")
	}
}

// isCredentialName mirrors the leaf names zarf_state_info withholds. See CREDENTIAL_NAMES and
// CREDENTIAL_SUBSTRINGS in the plugin, and docs/agent/choice-zarf-info-modules.md for why the
// match is by name rather than by a list of paths.
func isCredentialName(name string) bool {
	lowered := strings.ToLower(name)
	if lowered == "secret" || lowered == "key" {
		return true
	}
	return strings.Contains(lowered, "password") || strings.Contains(lowered, "token")
}
