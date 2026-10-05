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
	"github.com/stretchr/testify/require"
)

func TestUpgradeControllerPrepareMarksTheLeader(t *testing.T) {
	d := fakeServiceDistro{}
	running := newServiceHost("controller-running", true, true)
	other := newServiceHost("controller-other", true, true)
	running.Metadata.DistroVersion = "v1.34.0"
	other.Metadata.DistroVersion = "v1.34.0"

	p := &UpgradeController{}
	p.Distro = d
	p.SetManager(&Manager{Config: &cluster.ZarfCluster{Spec: cluster.ZarfClusterSpec{Hosts: cluster.ZarfHosts{running, other}}}})

	dis := &distro.ZarfDistro{Spec: distro.ZarfDistroSpec{Version: "v1.35.0"}}
	require.NoError(t, p.Prepare(context.Background(), nil, dis))

	require.True(t, running.Metadata.IsLeader)
	require.False(t, other.Metadata.IsLeader)
	require.Same(t, dis, p.dis)
}
