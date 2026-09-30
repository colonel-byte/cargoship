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
	"context"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/stretchr/testify/require"
)

// fakeVersionDistro reports a fixed RunningVersion, so a test can drive
// investigateHostDistro's downgrade/skew checks without a real host.
type fakeVersionDistro struct {
	distrocfg.Distro
	version string
	err     error
}

func (f *fakeVersionDistro) RunningVersion(*cluster.ZarfHost) (string, error) {
	return f.version, f.err
}

func TestInvestigateHostDistroRejectsTwoMinorVersionJump(t *testing.T) {
	p := &GatherFactsDistro{
		Distro: &fakeVersionDistro{version: "v1.33.0"},
		d:      &distro.ZarfDistro{Spec: distro.ZarfDistroSpec{Version: "v1.35.0"}},
	}
	h := &cluster.ZarfHost{}

	err := p.investigateHostDistro(context.Background(), h)

	require.ErrorContains(t, err, "will not upgrade")
	require.ErrorContains(t, err, "more than one minor version at a time")
}

func TestInvestigateHostDistroAllowsOneMinorVersionJump(t *testing.T) {
	p := &GatherFactsDistro{
		Distro: &fakeVersionDistro{version: "v1.34.0"},
		d:      &distro.ZarfDistro{Spec: distro.ZarfDistroSpec{Version: "v1.35.0"}},
	}
	h := &cluster.ZarfHost{}

	require.NoError(t, p.investigateHostDistro(context.Background(), h))
	require.Equal(t, "v1.34.0", h.Metadata.DistroVersion)
}

func TestInvestigateHostDistroRejectsDowngrade(t *testing.T) {
	p := &GatherFactsDistro{
		Distro: &fakeVersionDistro{version: "v1.35.0"},
		d:      &distro.ZarfDistro{Spec: distro.ZarfDistroSpec{Version: "v1.34.0"}},
	}
	h := &cluster.ZarfHost{}

	err := p.investigateHostDistro(context.Background(), h)

	require.ErrorContains(t, err, "will not downgrade")
}

func TestInvestigateHostDistroAllowsFreshInstall(t *testing.T) {
	p := &GatherFactsDistro{
		Distro: &fakeVersionDistro{err: distrocfg.ErrVersionNotDetected},
		d:      &distro.ZarfDistro{Spec: distro.ZarfDistroSpec{Version: "v1.37.0"}},
	}
	h := &cluster.ZarfHost{}

	require.NoError(t, p.investigateHostDistro(context.Background(), h))
	require.Equal(t, UnknownVersion, h.Metadata.DistroVersion)
}
