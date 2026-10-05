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
	"errors"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/stretchr/testify/require"
)

// fakePreStartUpgraderDistro is a Distro that implements PreStartUpgrader, matching
// upstream/kubeadm's mid-sequence upgrade step.
type fakePreStartUpgraderDistro struct {
	fakeServiceDistro
	calls []string
	err   error
}

func (f *fakePreStartUpgraderDistro) PreStartUpgrade(_ context.Context, h *cluster.ZarfHost, _ distro.ZarfDistro) error {
	f.calls = append(f.calls, h.Hostname)
	return f.err
}

func TestPreStartUpgradeNoOpWhenNotImplemented(t *testing.T) {
	p := &UpgradeHosts{Distro: fakeServiceDistro{}, dis: &distro.ZarfDistro{}}
	h := &cluster.ZarfHost{Hostname: "node1"}

	require.NoError(t, p.preStartUpgrade(context.Background(), h))
}

func TestPreStartUpgradeDispatchesWhenImplemented(t *testing.T) {
	d := &fakePreStartUpgraderDistro{}
	p := &UpgradeHosts{Distro: d, dis: &distro.ZarfDistro{}}
	h := &cluster.ZarfHost{Hostname: "node1"}

	require.NoError(t, p.preStartUpgrade(context.Background(), h))
	require.Equal(t, []string{"node1"}, d.calls)
}

func TestPreStartUpgradePropagatesError(t *testing.T) {
	wantErr := errors.New("kubeadm upgrade apply failed")
	d := &fakePreStartUpgraderDistro{err: wantErr}
	p := &UpgradeHosts{Distro: d, dis: &distro.ZarfDistro{}}
	h := &cluster.ZarfHost{Hostname: "node1"}

	err := p.preStartUpgrade(context.Background(), h)

	require.ErrorIs(t, err, wantErr)
}
