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
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The fixtures the two read-only package modules are driven over.
const (
	packageInfoPlaybook      = "test/e2e/noncluster/testdata/zarf-package-info.yml"
	packageInfoEmptyPlaybook = "test/e2e/noncluster/testdata/zarf-package-info-empty.yml"
	packageListFixture       = "test/e2e/noncluster/testdata/zarf-package-list.json"
	packageListEmptyFixture  = "test/e2e/noncluster/testdata/zarf-package-list-empty.json"
	packageListStub          = "test/e2e/noncluster/testdata/zarf-package-list-stub.sh"

	packageInspectPlaybook = "test/e2e/noncluster/testdata/zarf-package-inspect.yml"
	inspectFixtureDir      = "test/e2e/noncluster/testdata/zarf-inspect-fixture"
)

// envPackageList names the document the package list stub serves.
const envPackageList = "CARGOSHIP_E2E_ZARF_PACKAGE_LIST"

// The plugins the playbooks exercise. They are read before the playbooks run, for the reason given
// at stateInfoPlugin: Go's test cache keys on what the test process opens, and everything these
// tests exercise is opened by the ansible-playbook child.
const (
	packageInfoPlugin    = "ansible/colonel_byte/zarf/plugins/action/zarf_package_info.py"
	packageInspectPlugin = "ansible/colonel_byte/zarf/plugins/action/zarf_package_inspect.py"
	infoBasePlugin       = "ansible/colonel_byte/zarf/plugins/plugin_utils/info.py"
)

// TestZarfPackageInfoDecodesTheList holds what zarf_package_info does with what zarf prints.
//
// The module's one impure step is running zarf, so a stub that prints a recorded document covers
// everything else. The document's shape is packageListInfo in zarf's src/cmd/package.go rather than
// a guess: package, namespaceOverride, version, connectivity, components.
//
// The empty case has a playbook of its own because it is a different document. `zarf package list`
// marshals a nil slice when nothing is deployed, so it prints `null` and not `[]`, and the module
// has a branch for it.
func TestZarfPackageInfoDecodesTheList(t *testing.T) {
	for _, tt := range []struct {
		name     string
		playbook string
		fixture  string
	}{
		{
			name:     "a cluster with packages deployed",
			playbook: packageInfoPlaybook,
			fixture:  packageListFixture,
		},
		{
			name:     "a cluster with nothing deployed",
			playbook: packageInfoEmptyPlaybook,
			fixture:  packageListEmptyFixture,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := zarfPlaybookEnv(t, []string{packageInfoPlugin, infoBasePlugin})
			stub := env.executable(t, packageListStub)

			runZarfPlaybook(t, env, tt.playbook, []string{
				envZarfStub + "=" + stub,
				envPackageList + "=" + filepath.Join(env.root, tt.fixture),
			}, nil)
		})
	}
}

// TestZarfPackageInspectParsesAndVerifies holds what zarf_package_inspect reports, and that the
// verify parameter reaches zarf.
//
// It needs a real zarf and no cluster: a package on disk is a complete input, and only deploying
// one needs a cluster. The package is built here from testdata/zarf-inspect-fixture rather than
// committed as a tarball -- a .tar.zst is a binary blob nobody can review, and what matters is the
// definition the module is expected to report back, which is a document in that directory.
func TestZarfPackageInspectParsesAndVerifies(t *testing.T) {
	zarf, err := exec.LookPath("zarf")
	if err != nil {
		t.Skip("zarf is not on PATH: the module runs the installed zarf rather than embedding one")
	}

	env := zarfPlaybookEnv(t, []string{packageInspectPlugin, infoBasePlugin})

	// A key whose contents do not matter. The package is unsigned, so zarf refuses it for having no
	// signature before anything reads the key -- which is the case under test.
	key := filepath.Join(t.TempDir(), "unused.pub")
	require.NoError(t, os.WriteFile(key, []byte("not a key, and never read\n"), 0o600))

	runZarfPlaybook(t, env, packageInspectPlaybook, nil, []string{
		"-e", "zarf_package=" + buildInspectFixture(t, env.root, zarf),
		"-e", "zarf_binary=" + zarf,
		// Not named zarf_public_key: an extra-var of that name would be the highest-precedence
		// value of it and would reach the role include that is meant to run without a key.
		"-e", "inspect_public_key=" + key,
	})
}

// buildInspectFixture builds the fixture package and returns the tarball's path.
//
// `zarf package create` over a files-only component reaches no registry and no network, which is
// what keeps these tests in the noncluster suite. The output name is zarf's own convention, so it
// is found by a glob rather than spelled out: it carries the architecture of the host that built it.
func buildInspectFixture(t *testing.T, root, zarf string) string {
	t.Helper()

	out := t.TempDir()
	cmd := exec.Command(zarf, "package", "create", filepath.Join(root, inspectFixtureDir), //nolint:gosec // the suite's own constants
		"--confirm", "--output", out, "--no-color")
	if built, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("unable to build the inspect fixture package: %v\n%s", err, built)
	}

	matches, err := filepath.Glob(filepath.Join(out, "zarf-package-inspect-fixture-*.tar.zst"))
	require.NoError(t, err)
	require.Len(t, matches, 1, "the build produced %d packages rather than one", len(matches))
	return matches[0]
}
