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

package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDescribesItself guards the one exception to a fatal config file. Making an unknown key fatal
// is the point of this change, but it must not take the help with it: the error tells the operator
// to run "cargoship validate --kind config", and without this exception the help that would have
// shown them that command exited non-zero and printed nothing at all.
func TestDescribesItself(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "no arguments",
			args: nil,
			want: false,
		},
		{
			name: "long help flag",
			args: []string{"--help"},
			want: true,
		},
		{
			name: "short help flag",
			args: []string{"-h"},
			want: true,
		},
		{
			name: "help flag after a subcommand",
			args: []string{"create", "--help"},
			want: true,
		},
		{
			name: "help subcommand",
			args: []string{"help"},
			want: true,
		},
		{
			name: "version subcommand",
			args: []string{"version"},
			want: true,
		},
		{
			name: "version alias",
			args: []string{"v"},
			want: true,
		},
		{
			name: "completion subcommand",
			args: []string{"completion", "bash"},
			want: true,
		},
		{
			name: "a command that does real work",
			args: []string{"apply", "./pkg.tar.zst", "--confirm"},
			want: false,
		},
		{
			name: "version only in a flag value is not the version command",
			args: []string{"create", ".", "--tag", "version"},
			want: false,
		},
		{
			name: "a flag value in the first non-flag position is not recognised",
			args: []string{"--log-level", "debug", "version"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, describesItself(tt.args))
		})
	}
}
