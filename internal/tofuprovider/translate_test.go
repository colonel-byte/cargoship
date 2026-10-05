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

package tofuprovider

import (
	"context"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
)

// threeHosts is a configuration whose controller is not written first, which is what the ordering
// assertions below are about.
func threeHosts() clusterModel {
	return clusterModel{
		Name:         "bubbles",
		LoadBalancer: "10.0.0.10",
		Hosts: []hostModel{
			{
				Address: "10.0.0.21",
				Role:    cluster.RoleWorker,
			},
			{
				Address: "10.0.0.11",
				Role:    cluster.RoleController,
				KeyPath: "/home/operator/.ssh/id_ed25519",
			},
			{
				Address: "10.0.0.22",
				Role:    cluster.RoleWorker,
			},
		},
	}
}

// TestTranslateOrdersControllersFirst pins the ordering an apply depends on: ConfigureEngine makes
// the first controller the leader, so a configuration whose host blocks are in any other order
// would otherwise hand the leader role to a worker-shaped position.
func TestTranslateOrdersControllersFirst(t *testing.T) {
	cfg, err := translate(context.Background(), threeHosts())
	if err != nil {
		t.Fatalf("translate reported %v", err)
	}

	if len(cfg.Spec.Hosts) != 3 {
		t.Fatalf("the document holds %d hosts, want 3", len(cfg.Spec.Hosts))
	}
	if !cfg.Spec.Hosts[0].IsController() {
		t.Errorf("the first host is %q, want the controller first", cfg.Spec.Hosts[0].Role)
	}
	// The workers keep their configuration order, which is what makes a worker batch
	// reproducible between runs.
	if got := cfg.Spec.Hosts[1].ConnectionConfig.SSH.Address; got != "10.0.0.21" {
		t.Errorf("the second host is %s, want the first worker in configuration order", got)
	}
	if got := cfg.Spec.Hosts[2].ConnectionConfig.SSH.Address; got != "10.0.0.22" {
		t.Errorf("the third host is %s, want the second worker in configuration order", got)
	}
}

// TestTranslateAppliesRigDefaults covers what rig would otherwise write out as an empty user and
// port zero -- a document that fails its own schema. internal/ansibleinv writes the same defaults
// for the same reason.
func TestTranslateAppliesRigDefaults(t *testing.T) {
	cfg, err := translate(context.Background(), threeHosts())
	if err != nil {
		t.Fatalf("translate reported %v", err)
	}

	for _, host := range cfg.Spec.Hosts {
		ssh := host.ConnectionConfig.SSH
		if ssh.User != defaultSSHUser {
			t.Errorf("%s: user is %q, want the default %q", ssh.Address, ssh.User, defaultSSHUser)
		}
		if ssh.Port != defaultSSHPort {
			t.Errorf("%s: port is %d, want the default %d", ssh.Address, ssh.Port, defaultSSHPort)
		}
	}
}

// TestTranslateCarriesTheKeyPath is the other half of rule 1 of choice-tofu-secrets: the path
// reaches the host configuration, and there is nowhere for key material to reach.
func TestTranslateCarriesTheKeyPath(t *testing.T) {
	cfg, err := translate(context.Background(), threeHosts())
	if err != nil {
		t.Fatalf("translate reported %v", err)
	}

	controller := cfg.Spec.Hosts[0]
	if controller.ConnectionConfig.SSH.KeyPath == nil {
		t.Fatal("the controller carries no key path")
	}
	if got := *controller.ConnectionConfig.SSH.KeyPath; got != "/home/operator/.ssh/id_ed25519" {
		t.Errorf("the key path is %q", got)
	}

	for _, host := range cfg.Spec.Hosts[1:] {
		if host.ConnectionConfig.SSH.KeyPath != nil {
			t.Errorf("%s: a host block with no key_path was given one", host.ConnectionConfig.SSH.Address)
		}
	}
}

// TestTranslateRefusesIncompleteConfigurations covers what has to fail during a plan rather than
// minutes into an apply against live hosts. Each case is a configuration a practitioner can
// plausibly write.
func TestTranslateRefusesIncompleteConfigurations(t *testing.T) {
	cases := []struct {
		name  string
		model func(clusterModel) clusterModel
		wants string
	}{
		{
			name: "no name",
			model: func(m clusterModel) clusterModel {
				m.Name = ""
				return m
			},
			wants: "name is required",
		},
		{
			name: "no load balancer",
			model: func(m clusterModel) clusterModel {
				m.LoadBalancer = ""
				return m
			},
			wants: "load_balancer is required",
		},
		{
			name: "no hosts",
			model: func(m clusterModel) clusterModel {
				m.Hosts = nil
				return m
			},
			wants: "host block",
		},
		{
			name: "a host with no address",
			model: func(m clusterModel) clusterModel {
				m.Hosts[0].Address = ""
				return m
			},
			wants: "needs an address",
		},
		{
			name: "a host with no role",
			model: func(m clusterModel) clusterModel {
				m.Hosts[0].Role = ""
				return m
			},
			wants: "has no role",
		},
		{
			name: "a host with a role the API does not define",
			model: func(m clusterModel) clusterModel {
				m.Hosts[0].Role = "leader"
				return m
			},
			wants: "want one of",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := translate(context.Background(), tc.model(threeHosts()))
			if err == nil {
				t.Fatal("translate accepted the configuration")
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("the error was %q, want it to mention %q", err, tc.wants)
			}
		})
	}
}

// TestTranslateAcceptsEveryRole holds the schema's documented roles against the ones a translation
// accepts. The schema description is rendered from hostRoles, so a role added to the API and
// listed there has to translate as well.
func TestTranslateAcceptsEveryRole(t *testing.T) {
	for _, role := range hostRoles {
		t.Run(role, func(t *testing.T) {
			model := clusterModel{
				Name:         "bubbles",
				LoadBalancer: "10.0.0.10",
				Hosts: []hostModel{
					{
						Address: "10.0.0.11",
						Role:    role,
					},
				},
			}
			if _, err := translate(context.Background(), model); err != nil {
				t.Errorf("translate refused the documented role %q: %v", role, err)
			}
		})
	}
}
