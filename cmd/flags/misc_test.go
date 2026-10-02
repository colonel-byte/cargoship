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

package flags

import (
	"testing"

	"github.com/colonel-byte/cargoship/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestGetOutputFormat(t *testing.T) {
	values := getOutputFormat()
	require.Len(t, values, 2)
	require.Contains(t, values[0], string(config.OutputFormatJSON))
	require.Contains(t, values[1], string(config.OutputFormatYAML))
}

func TestRegisterOutputFormat(t *testing.T) {
	values, directive := RegisterOutputFormat(nil, nil, "")
	require.Equal(t, getOutputFormat(), values)
	require.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}
