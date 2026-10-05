// Copyright 2023 k0sctl authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
//
// Modifications Copyright 2026 colonel-byte.
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

package cluster

import (
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/sshconfig"
	"github.com/stretchr/testify/require"
)

func TestSSHConfigParserNoFinalizeTokens(t *testing.T) {
	// Tests that the WithNoFinalize parser workaround handles directives containing
	// tokens (%l, %C) without failing on unsupported token expansion during Finalize.
	configContent := `
Host example.com
    ControlPath ~/.ssh/control-%C-%l-%h
    Port 2222
`
	parser, err := sshconfig.NewParser(strings.NewReader(configContent), sshconfig.WithNoFinalize())
	require.NoError(t, err)

	sshCfg := &sshconfig.Config{}
	err = parser.Apply(sshCfg, "example.com")
	require.NoError(t, err)
	require.Equal(t, 2222, sshCfg.Port)
	require.Equal(t, "~/.ssh/control-%C-%l-%h", sshCfg.ControlPath)
}
