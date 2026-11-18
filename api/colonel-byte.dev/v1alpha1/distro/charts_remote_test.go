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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A chart fetched over a network is bytes nobody looked at, and it installs into the cluster
// with whatever its templates ask for -- so it has to name a digest, the same rule
// --file-override and --content-store are held to.
func TestValidateChartsRequiresAShasumForARemoteSource(t *testing.T) {
	for _, tt := range []struct {
		name    string
		chart   Chart
		wantErr string
	}{
		{
			name: "a url with no shasum",
			chart: Chart{
				Name:   "my-app",
				Source: "https://example.com/charts/my-app-1.2.3.tgz",
			},
			wantErr: "declares no shasum",
		},
		{
			name: "an oci tag with no shasum",
			chart: Chart{
				Name:    "my-app",
				Source:  "oci://registry.example.com/charts/my-app:1.2.3",
				Version: "1.2.3",
			},
			wantErr: "declares no shasum",
		},
		{
			name: "an oci reference pinned by digest",
			chart: Chart{
				Name:    "my-app",
				Source:  "oci://registry.example.com/charts/my-app@sha256:" + strings.Repeat("a", 64),
				Version: "1.2.3",
			},
		},
		{
			name: "a url with a shasum",
			chart: Chart{
				Name:   "my-app",
				Source: "https://example.com/charts/my-app-1.2.3.tgz",
				Shasum: "abc123",
			},
		},
		{
			name: "a local path with no shasum",
			chart: Chart{
				Name:   "my-app",
				Source: "charts/my-app-1.2.3.tgz",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{Charts: []Chart{tt.chart}}
			err := c.ValidateCharts("rke2")
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// An oci:// reference names a repository and a tag, not a file, so the tarball is named the way
// helm names a packaged chart. A source that already names one keeps it, so that what an
// operator finds in the static chart directory is what upstream published.
func TestChartTarballName(t *testing.T) {
	for _, tt := range []struct {
		name  string
		chart Chart
		want  string
	}{
		{
			name: "a local path",
			chart: Chart{
				Name:   "my-app",
				Source: "charts/my-app-1.2.3.tgz",
			},
			want: "my-app-1.2.3.tgz",
		},
		{
			name: "a url",
			chart: Chart{
				Name:   "my-app",
				Source: "https://example.com/d/my-app-1.2.3.tar.gz",
			},
			want: "my-app-1.2.3.tar.gz",
		},
		{
			name: "an oci reference",
			chart: Chart{
				Name:    "my-app",
				Source:  "oci://registry.example.com/charts/my-app:1.2.3",
				Version: "1.2.3",
			},
			want: "my-app-1.2.3.tgz",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.chart.TarballName())
		})
	}
}

// A source that names no tarball and a chart that names no version would stage "my-app-.tgz",
// which the fetch itself would not complain about.
func TestValidateChartsNeedsAVersionForAnOCISource(t *testing.T) {
	c := Config{
		Charts: []Chart{
			{
				Name:   "my-app",
				Source: "oci://registry.example.com/charts/my-app:1.2.3",
				Shasum: "abc123",
			},
		},
	}
	require.ErrorContains(t, c.ValidateCharts("rke2"), "names no version")
}
