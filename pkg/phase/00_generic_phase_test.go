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

package phase

import (
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

func TestVersionSkewTooGreat(t *testing.T) {
	for _, tt := range []struct {
		name    string
		running string
		target  string
		want    bool
	}{
		{name: "same minor", running: "v1.35.0", target: "v1.35.3", want: false},
		{name: "one minor ahead", running: "v1.34.0", target: "v1.35.0", want: false},
		{name: "two minors ahead", running: "v1.33.0", target: "v1.35.0", want: true},
		{name: "major version change", running: "v1.35.0", target: "v2.0.0", want: true},
		{name: "unparseable running version", running: "not-a-version", target: "v1.35.0", want: false},
		{name: "unparseable target version", running: "v1.35.0", target: "not-a-version", want: false},
		{name: "fresh install has no version yet", running: UnknownVersion, target: "v1.37.0", want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := &GenericPhase{}
			h := &cluster.ZarfHost{}
			h.Metadata.DistroVersion = tt.running

			require.Equal(t, tt.want, p.VersionSkewTooGreat(h, tt.target))
		})
	}
}
