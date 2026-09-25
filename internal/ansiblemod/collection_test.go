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

package ansiblemod

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// collectionRoot is the Ansible collection that presents these modules to a playbook.
const collectionRoot = "../../ansible/colonel_byte/cargoship"

// TestCollectionCoversEveryModule is what stops a module shipping that no playbook can reach.
//
// A module needs three things outside this package before it works: an action plugin, so Ansible
// has something to load and the inventory gets projected into the arguments; a symlink from the
// distribution package, so there is a module file at all; and a task file in the role. Each one is
// in a different file in a different language, and a module missing any of them fails at the point
// an operator runs a play against a live fleet.
func TestCollectionCoversEveryModule(t *testing.T) {
	for _, tt := range []struct {
		what string
		got  func(t *testing.T) []string
	}{
		{what: "action plugins", got: actionPlugins},
		{what: "role task files", got: roleTasks},
		{what: "packaged module symlinks", got: packagedModules},
	} {
		t.Run(tt.what, func(t *testing.T) {
			require.Equal(t, Modules(), tt.got(t))
		})
	}
}

func actionPlugins(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(collectionRoot, "plugins", "action"))
	require.NoError(t, err)

	var names []string
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".py")
		if after, ok := strings.CutPrefix(name, Prefix); ok {
			names = append(names, after)
		}
	}
	sort.Strings(names)
	return names
}

func roleTasks(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(collectionRoot, "roles", "cluster", "tasks"))
	require.NoError(t, err)

	var names []string
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".yml")
		// main.yml dispatches to the rest, and is not an action.
		if name != "main" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func packagedModules(t *testing.T) []string {
	t.Helper()
	config, err := os.ReadFile("../../.goreleaser.yaml")
	require.NoError(t, err)

	var names []string
	for _, line := range strings.Split(string(config), "\n") {
		_, path, ok := strings.Cut(strings.TrimSpace(line), "plugins/modules/"+Prefix)
		if ok {
			names = append(names, path)
		}
	}
	sort.Strings(names)
	return names
}
