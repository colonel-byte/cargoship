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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// envZarfStub names the stand-in zarf a playbook passes as the module's zarf_binary.
const envZarfStub = "CARGOSHIP_E2E_ZARF_STUB"

// assertedPrefix is what every assert task in these playbooks says when it passes.
//
// The assertions about returned data live in the playbooks, because they are about the contents of
// a dictionary and reading one back into Go would be a second copy of the same expectations. What
// Go holds is that every one of those tasks ran: a play that stopped early, or a renamed fact that
// skipped a block, leaves the remaining assertions passing and the playbook exiting 0.
const assertedPrefix = "ASSERTED "

// zarfPlaybookEnvironment is what one of these playbooks needs to run: the repository root, the
// collections path Ansible insists on, and the ansible-playbook to run it with.
type zarfPlaybookEnvironment struct {
	root        string
	collections string
	ansible     string
}

// zarfPlaybookEnv prepares that, skipping the test when Ansible is not installed.
//
// The plugins named in readFirst are read here and not parsed. That is the whole reason: Go's test
// cache keys on the files the test process opens, and everything one of these tests exercises is
// opened by the ansible-playbook child instead. Without reading them, an edit to a plugin leaves a
// passing result cached -- which is the shape of mistake that gets a credential leak marked as
// fixed. Confirmed by sabotaging a plugin: before the reads the cached pass survived, after them
// the test fails as it should.
func zarfPlaybookEnv(t *testing.T, readFirst []string) zarfPlaybookEnvironment {
	t.Helper()

	ansible, err := exec.LookPath("ansible-playbook")
	if err != nil {
		t.Skip("ansible-playbook is not on PATH: these modules are action plugins, which only " +
			"Ansible can run")
	}

	root, err := os.Getwd()
	require.NoError(t, err)

	// The layout Ansible insists on and the repository does not have: a collections path holding an
	// ansible_collections directory, whose children are the namespaces.
	collections := t.TempDir()
	require.NoError(t, os.Symlink(
		filepath.Join(root, "ansible"),
		filepath.Join(collections, "ansible_collections"),
	))

	for _, path := range readFirst {
		_, err := os.ReadFile(filepath.Join(root, path)) //nolint:gosec // the suite's own constants
		require.NoError(t, err, "unable to read %s", path)
	}

	return zarfPlaybookEnvironment{root: root, collections: collections, ansible: ansible}
}

// executable returns the absolute path of one of the suite's stub binaries, having checked that it
// is actually executable: a stub committed without its bit set fails as a module error about a
// missing zarf, which reads as the module being broken.
func (e zarfPlaybookEnvironment) executable(t *testing.T, path string) string {
	t.Helper()

	absolute := filepath.Join(e.root, path)
	info, err := os.Stat(absolute)
	require.NoError(t, err, "%s is missing", path)
	require.NotZero(t, info.Mode()&0o111, "%s is not executable", path)
	return absolute
}

// runZarfPlaybook runs one playbook and requires it to pass, with every assert task in it having
// reported passing. It returns the playbook's output.
//
// How many assertions to expect is read out of the playbook rather than passed in, so that adding
// one does not quietly stop the count from meaning anything.
func runZarfPlaybook(t *testing.T, env zarfPlaybookEnvironment, playbook string, extraEnv, extraArgs []string) string {
	t.Helper()

	source, err := os.ReadFile(filepath.Join(env.root, playbook)) //nolint:gosec // the suite's own constants
	require.NoError(t, err)
	expected := strings.Count(string(source), "success_msg: "+assertedPrefix)
	require.NotZero(t, expected, "%s holds no assertions", playbook)

	argv := append([]string{"-i", "localhost,", filepath.Join(env.root, playbook)}, extraArgs...)
	cmd := exec.Command(env.ansible, argv...) //nolint:gosec // the path is from LookPath
	cmd.Dir = env.root
	cmd.Env = append(os.Environ(), "ANSIBLE_COLLECTIONS_PATH="+env.collections)
	// The playbooks assert on what a module returned, and a localhost fact gather is both slow and
	// irrelevant to that.
	cmd.Env = append(cmd.Env, "ANSIBLE_GATHERING=explicit")
	cmd.Env = append(cmd.Env, extraEnv...)

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "the playbook failed:\n%s", out)

	if asserted := strings.Count(string(out), assertedPrefix); asserted != expected {
		t.Errorf("%d of %s's %d assert tasks reported passing, so some of them did not run:\n%s",
			asserted, playbook, expected, out)
	}
	return string(out)
}
