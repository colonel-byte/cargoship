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
	"context"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
)

// TestNewManagerBare covers the no-package path: the manager reset and kube-config build, and the
// one an out-of-tree caller builds when it has an inventory and an engine name and nothing else.
// The loaded-package path needs a real extracted package, so the e2e phase harness is what
// exercises it.
func TestNewManagerBare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		opts            ManagerOptions
		wantConcurrency int
		wantTimeout     time.Duration
	}{
		{
			name: "distro id and concurrency",
			opts: ManagerOptions{
				DistroID:    "k3s",
				Concurrency: 4,
			},
			wantConcurrency: 4,
		},
		{
			name: "negative concurrency is clamped to unlimited",
			opts: ManagerOptions{
				DistroID:    "k3s",
				Concurrency: -1,
			},
			wantConcurrency: 0,
		},
		{
			name: "timeout is applied",
			opts: ManagerOptions{
				DistroID:    "k3s",
				Concurrency: 1,
				Timeout:     90 * time.Second,
			},
			wantConcurrency: 1,
			wantTimeout:     90 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m, err := NewManager(context.Background(), tt.opts)
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}

			if m.DistroID != tt.opts.DistroID {
				t.Errorf("DistroID = %q, want %q", m.DistroID, tt.opts.DistroID)
			}
			if m.Concurrency != tt.wantConcurrency {
				t.Errorf("Concurrency = %d, want %d", m.Concurrency, tt.wantConcurrency)
			}
			// Uploads track Concurrency; the CLI set both from one flag and a caller that got
			// only one of them would see a phase quietly act on more hosts than it asked for.
			if m.ConcurrentUploads != tt.wantConcurrency {
				t.Errorf("ConcurrentUploads = %d, want %d", m.ConcurrentUploads, tt.wantConcurrency)
			}
			if tt.wantTimeout > 0 && m.Timeout != tt.wantTimeout {
				t.Errorf("Timeout = %s, want %s", m.Timeout, tt.wantTimeout)
			}
			// No package was loaded, so there is nothing for the caller to remove.
			if m.TempDirectory != "" {
				t.Errorf("TempDirectory = %q, want empty with no layout", m.TempDirectory)
			}
			if m.Distro != nil {
				t.Error("Distro is set with no layout")
			}
		})
	}
}

// TestNewManagerConfigIsTheGivenInventory pins that the manager points at the inventory it was
// handed. It is worth a test because Config is a pointer into the options struct: a caller that
// built hosts in memory and then found the manager holding a different inventory would have no way
// to tell until a phase acted on the wrong fleet.
func TestNewManagerConfigIsTheGivenInventory(t *testing.T) {
	t.Parallel()

	inventory := cluster.Cluster{
		Spec: cluster.Spec{
			Hosts: cluster.Hosts{
				{
					Role:     "controller",
					Hostname: "cp-0",
				},
			},
		},
	}
	inventory.Metadata.Name = "fleet"

	m, err := NewManager(context.Background(), ManagerOptions{
		Cluster:  inventory,
		DistroID: "rke2",
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if m.Config == nil {
		t.Fatal("Config is nil")
	}
	if m.Config.Metadata.Name != "fleet" {
		t.Errorf("Config.Metadata.Name = %q, want %q", m.Config.Metadata.Name, "fleet")
	}
	if len(m.Config.Spec.Hosts) != 1 || m.Config.Spec.Hosts[0].Hostname != "cp-0" {
		t.Errorf("Config.Spec.Hosts = %+v, want the one host given", m.Config.Spec.Hosts)
	}
}
