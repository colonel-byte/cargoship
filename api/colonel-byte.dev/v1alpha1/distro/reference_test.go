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

package distro

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateReferenceLengths(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		pkgName string
		version string
		wantErr string
	}{
		{
			name:    "the longest name in this repository, with room to spare",
			pkgName: "rancher-k3s-multi",
			version: "1.36.4-k3s1",
		},
		{
			name:    "exactly at both bounds",
			pkgName: strings.Repeat("a", MaxNameLength),
			version: strings.Repeat("1", MaxVersionLength),
		},
		{
			name:    "one over on the name",
			pkgName: strings.Repeat("a", MaxNameLength+1),
			version: "1.0.0",
			wantErr: "the package name is 65 characters, over the 64 allowed",
		},
		{
			name:    "one over on the version",
			pkgName: "rke2",
			version: strings.Repeat("1", MaxVersionLength+1),
			wantErr: "the package version is 49 characters, over the 48 allowed",
		},
		{
			name:    "no version is this function's business",
			pkgName: "rke2",
			version: "",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateReferenceLengths(tt.pkgName, tt.version)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestBoundsMatchTheSchemaTags is the anti-drift check. The bound is stated twice -- once as a
// constant the runtime check reads, once as a maxLength in a struct tag the generated schema
// reads -- because a struct tag cannot interpolate a constant. Two numbers that must agree and
// are edited in different places is exactly the pair that silently diverges, so this reads the
// tag back and compares.
func TestBoundsMatchTheSchemaTags(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		field string
		want  int
	}{
		{
			field: "Name",
			want:  MaxNameLength,
		},
		{
			field: "Version",
			want:  MaxVersionLength,
		},
	} {
		t.Run(tt.field, func(t *testing.T) {
			t.Parallel()

			f, ok := reflect.TypeFor[Metadata]().FieldByName(tt.field)
			require.True(t, ok, "Metadata should have a %s field", tt.field)
			require.Equal(t, fmt.Sprintf("maxLength=%d", tt.want), maxLengthTag(t, f.Tag.Get("jsonschema")),
				"the jsonschema maxLength on %s and its constant have diverged", tt.field)
		})
	}
}

// maxLengthTag pulls the maxLength clause out of a jsonschema tag.
func maxLengthTag(t *testing.T, tag string) string {
	t.Helper()

	for _, part := range strings.Split(tag, ",") {
		if strings.HasPrefix(part, "maxLength=") {
			return part
		}
	}
	require.Failf(t, "no maxLength", "the jsonschema tag %q declares no maxLength", tag)
	return ""
}
