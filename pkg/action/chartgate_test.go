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

package action

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Only an explicit false may gate a chart off. Each of the other three answers used to be
// collapsed into false, which prunes the chart's manifest and so uninstalls a running release:
// a renamed values key would have taken production down and logged one warning line.
func TestChartGate(t *testing.T) {
	values := map[string]any{
		"chart": map[string]any{
			"enabled":  true,
			"disabled": false,
			"stringy":  "true",
		},
	}

	for _, tc := range []struct {
		name    string
		path    string
		want    bool
		wantErr bool
	}{
		{
			name: "explicitly on",
			path: ".chart.enabled",
			want: true,
		},
		{
			name: "explicitly off",
			path: ".chart.disabled",
			want: false,
		},
		{
			name:    "key renamed out from under the gate",
			path:    ".chart.gone",
			wantErr: true,
		},
		{
			name:    "whole parent map replaced by an override",
			path:    ".somethingElse.enabled",
			wantErr: true,
		},
		{
			name:    "a string that looks like a boolean",
			path:    ".chart.stringy",
			wantErr: true,
		},
		{
			name:    "not a value path at all",
			path:    "chart.enabled",
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := chartGate(values, tc.path)
			if tc.wantErr {
				require.Error(t, err)
				require.False(t, got, "an unreadable gate must not deliver a chart either")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
