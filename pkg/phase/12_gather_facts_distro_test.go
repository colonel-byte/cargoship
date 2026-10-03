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
	rig "github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/protocol/ssh"
	"github.com/stretchr/testify/require"
)

// downgradeHost is a host the refusal can name: a ZarfHost with no connection configuration
// stringifies to "unknown{}", which would make the assertions below pass on a message that tells
// an operator nothing.
func downgradeHost(address string) *cluster.ZarfHost {
	return &cluster.ZarfHost{
		ClientWithConfig: rig.ClientWithConfig{
			ConnectionConfig: rig.CompositeConfig{
				SSH: &ssh.Config{Address: address},
			},
		},
	}
}

// runningVersionDistro reports a fixed running version, or ErrVersionNotDetected when the version
// is empty, which is what a host with no engine on it produces.
type runningVersionDistro struct {
	distrocfg.Distro

	version string
}

func (d *runningVersionDistro) RunningVersion(_ *cluster.ZarfHost) (string, error) {
	if d.version == "" {
		return "", distrocfg.ErrVersionNotDetected
	}
	return d.version, nil
}

// TestInvestigateHostDistroDowngrade covers the refusal #307 is about. The comparison is against
// the version the host is already running, so the cases that matter are the three orderings plus
// the host that is running nothing.
func TestInvestigateHostDistroDowngrade(t *testing.T) {
	cases := []struct {
		name           string
		running        string
		packaged       string
		allowDowngrade bool
		wantErr        bool
		wantVersion    string
	}{
		{
			name:        "host runs a newer version",
			running:     "v1.34.1+k3s1",
			packaged:    "v1.33.4+k3s1",
			wantErr:     true,
			wantVersion: "v1.34.1+k3s1",
		},
		{
			name:        "host runs the packaged version",
			running:     "v1.33.4+k3s1",
			packaged:    "v1.33.4+k3s1",
			wantErr:     false,
			wantVersion: "v1.33.4+k3s1",
		},
		{
			name:        "host runs an older version",
			running:     "v1.33.4+k3s1",
			packaged:    "v1.34.1+k3s1",
			wantErr:     false,
			wantVersion: "v1.33.4+k3s1",
		},
		{
			name:        "host runs nothing",
			running:     "",
			packaged:    "v1.33.4+k3s1",
			wantErr:     false,
			wantVersion: UnknownVersion,
		},
		{
			name:           "newer version allowed explicitly",
			running:        "v1.34.1+k3s1",
			packaged:       "v1.33.4+k3s1",
			allowDowngrade: true,
			wantErr:        false,
			wantVersion:    "v1.34.1+k3s1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &GatherFactsDistro{
				Distro:         &runningVersionDistro{version: tc.running},
				AllowDowngrade: tc.allowDowngrade,
				d:              &distro.ZarfDistro{Spec: distro.ZarfDistroSpec{Version: tc.packaged}},
			}
			h := downgradeHost("10.0.0.11")

			err := p.investigateHostDistro(context.Background(), h)

			if tc.wantErr {
				require.ErrorIs(t, err, ErrWillNotDowngrade)
				// The message is the whole output of a run that stops here, so it has to name
				// which host and which two versions, not only that something was refused.
				require.Contains(t, err.Error(), "10.0.0.11")
				require.Contains(t, err.Error(), tc.running)
				require.Contains(t, err.Error(), tc.packaged)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantVersion, h.Metadata.DistroVersion)
		})
	}
}

// TestInvestigateHostDistroWithoutAPackage pins the case reset and kube-config rely on: they load
// no package, so there is no version to compare against and the phase must only record what it
// found.
func TestInvestigateHostDistroWithoutAPackage(t *testing.T) {
	p := &GatherFactsDistro{Distro: &runningVersionDistro{version: "v1.34.1+k3s1"}}
	h := downgradeHost("10.0.0.11")

	require.NoError(t, p.investigateHostDistro(context.Background(), h))
	require.Equal(t, "v1.34.1+k3s1", h.Metadata.DistroVersion)
}
