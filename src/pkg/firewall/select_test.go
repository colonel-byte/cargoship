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

package firewall

import (
	"context"
	"testing"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

// fakeBackend stands in for a real backend so the selection rules can be exercised without a
// host to run commands on.
type fakeBackend struct {
	name string
}

func (f *fakeBackend) Name() string                       { return f.name }
func (f *fakeBackend) Detect(_ *cluster.ZarfHost) bool    { return false }
func (f *fakeBackend) Installed(_ *cluster.ZarfHost) bool { return false }
func (f *fakeBackend) Apply(_ context.Context, _ *cluster.ZarfHost, _ Plan) error {
	return nil
}

func TestSelectBackend(t *testing.T) {
	firewalld := &fakeBackend{name: FirewalldService}
	ufw := &fakeBackend{name: UFWService}
	nftables := &fakeBackend{name: NftablesService}
	available := []Backend{firewalld, ufw, nftables}

	tests := []struct {
		name string
		// preferred is the firewall the host's OS ships.
		preferred string
		// running lists the backends whose Detect is true on the host.
		running []string
		// installed lists the backends present on the host, running or not.
		installed   []string
		wantBackend Backend
		wantSkipped Backend
	}{
		{
			name:        "the preferred firewall is used when it is running",
			preferred:   FirewalldService,
			running:     []string{FirewalldService, NftablesService},
			installed:   []string{FirewalldService, NftablesService},
			wantBackend: firewalld,
		},
		{
			name:        "an installed but stopped preferred firewall leaves the host alone",
			preferred:   UFWService,
			running:     []string{NftablesService},
			installed:   []string{UFWService, NftablesService},
			wantSkipped: ufw,
		},
		{
			name:        "a host without its preferred firewall falls through to nftables",
			preferred:   UFWService,
			running:     []string{NftablesService},
			installed:   []string{NftablesService},
			wantBackend: nftables,
		},
		{
			name:        "an OS with no preferred firewall matches in order",
			running:     []string{UFWService, NftablesService},
			installed:   []string{UFWService, NftablesService},
			wantBackend: ufw,
		},
		{
			name:      "a host running no firewall matches nothing",
			preferred: FirewalldService,
		},
		{
			name:      "a preferred firewall cargoship has no backend for is ignored",
			preferred: "pf",
			installed: []string{"pf"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contains := func(names []string) func(Backend) bool {
				return func(b Backend) bool {
					for _, name := range names {
						if name == b.Name() {
							return true
						}
					}

					return false
				}
			}

			got := selectBackend(available, tt.preferred, contains(tt.running), contains(tt.installed))
			require.Equal(t, tt.wantBackend, got.Backend)
			require.Equal(t, tt.wantSkipped, got.Skipped)
		})
	}
}
