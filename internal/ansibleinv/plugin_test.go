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

package ansibleinv

import (
	"os"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

// pluginPath is the one piece of Python in the collection. It projects hostvars down to the
// variables cargoship reads, which means it holds a copy of the list below.
const pluginPath = "../../ansible/colonel_byte/cargoship/plugins/plugin_utils/projection.py"

// TestActionPluginVariableAllowlist keeps that copy true.
//
// The plugin drops the host variables cargoship does not read rather than forwarding them, because
// a host's resolved variables routinely carry credentials for unrelated things and a module's
// arguments are written to a file on disk. That filter is only correct while it names the same
// variables this package does: a variable added here and not there would be read from an inventory
// that never reaches the translation, and the symptom would be an unset field rather than an error.
func TestActionPluginVariableAllowlist(t *testing.T) {
	source, err := os.ReadFile(pluginPath)
	require.NoError(t, err)

	tuple := regexp.MustCompile(`(?s)ANSIBLE_VARS = \((.*?)\)`).FindSubmatch(source)
	require.Len(t, tuple, 2, "the plugin no longer declares ANSIBLE_VARS as a tuple literal")

	got := regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(string(tuple[1]), -1)
	names := make([]string, 0, len(got))
	for _, m := range got {
		names = append(names, m[1])
	}
	sort.Strings(names)

	want := []string{VarAnsibleHost, VarAnsibleKeyFile, VarAnsiblePort, VarAnsibleUser}
	sort.Strings(want)
	require.Equal(t, want, names)

	// Every cargoship_ variable is forwarded whatever its spelling, because rejecting an unknown
	// one by name is this package's job and it cannot do that without being shown the misspelling.
	require.Contains(t, string(source), `CARGOSHIP_PREFIX = "`+Prefix+`"`)
}
