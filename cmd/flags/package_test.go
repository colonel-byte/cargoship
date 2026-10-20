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

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestGetOCIConcurrency(t *testing.T) {
	values := getOCIConcurrency()
	require.Len(t, values, 3)
	require.Contains(t, values[0], "1\t")
	require.Contains(t, values[1], "6\t")
	require.Contains(t, values[2], "10\t")
}

func TestRegisterOCIConcurrency(t *testing.T) {
	values, directive := RegisterOCIConcurrency(nil, nil, "")
	require.Equal(t, getOCIConcurrency(), values)
	require.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}
