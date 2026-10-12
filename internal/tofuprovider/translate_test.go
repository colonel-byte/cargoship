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

// profiledCluster is a fleet shaped the way the Ansible fleet example is: a control profile with
// a port and a label, an infra profile with a taint, and one worker that selects infra while the
// rest take the general profile.
func profiledCluster() clusterModel {
	model := threeHosts()
	model.Profiles = map[string]profileModel{
		"control": {
			NodeLabels: map[string]string{"adrp.xyz/purpose-control": "true"},
			Ports: []portModel{
				{Port: "6443"},
			},
			Concurrency: "1",
		},
		"general": {},
		"infra": {
			NodeTaints: []string{"adrp.xyz/infra=true:NoSchedule"},
			FirewallRules: []firewallRuleModel{
				{
					Name:   "allow-backup",
					Action: "allow",
					Source: "10.0.0.0/8",
					Port:   "2049",
				},
			},
		},
	}
	model.Hosts[0].Profile = "infra"
	model.Hosts[1].Profile = "control"
	model.Hosts[2].Profile = "general"
	return model
}

// TestTranslateRendersProfiles is the question this slice answers: a profile defined once in the
// configuration reaches the cluster document, so a host selecting it gets the labels, taints,
// ports and concurrency the profile carries rather than only the node-role label its name implies.
func TestTranslateRendersProfiles(t *testing.T) {
	cfg, err := translate(context.Background(), profiledCluster())
	if err != nil {
		t.Fatalf("translate reported %v", err)
	}

	profiles := cfg.Spec.Config.Profiles
	if len(profiles) != 3 {
		t.Fatalf("the document holds %d profiles, want 3", len(profiles))
	}

	control := profiles["control"]
	if got := control.Engine.NodeLabels["adrp.xyz/purpose-control"]; got != "true" {
		t.Errorf("the control profile's label did not reach the document: %q", got)
	}
	if len(control.Host.Ports) != 1 || control.Host.Ports[0].Port != "6443" {
		t.Errorf("the control profile's ports are %+v", control.Host.Ports)
	}
	// A port that names no protocol is tcp, which is what every port a cluster opens uses and
	// what the API requires to be stated.
	if got := control.Host.Ports[0].Protocol; got != defaultProtocol {
		t.Errorf("the port's protocol defaulted to %q, want %q", got, defaultProtocol)
	}
	if control.Concurrency != "1" {
		t.Errorf("the control profile's concurrency is %q", control.Concurrency)
	}

	infra := profiles["infra"]
	if len(infra.Engine.NodeTaints) != 1 || infra.Engine.NodeTaints[0] != "adrp.xyz/infra=true:NoSchedule" {
		t.Errorf("the infra profile's taints are %+v", infra.Engine.NodeTaints)
	}
	if len(infra.Host.Firewall.Rules) != 1 || infra.Host.Firewall.Rules[0].Action != "allow" {
		t.Errorf("the infra profile's firewall rules are %+v", infra.Host.Firewall.Rules)
	}

	// The hosts carry the selection, which is what the phases look up by.
	byProfile := map[string]string{}
	for _, host := range cfg.Spec.Hosts {
		byProfile[host.ConnectionConfig.SSH.Address] = host.Profile
	}
	if byProfile["10.0.0.21"] != "infra" {
		t.Errorf("the worker's profile is %q, want infra", byProfile["10.0.0.21"])
	}
	if byProfile["10.0.0.11"] != "control" {
		t.Errorf("the controller's profile is %q, want control", byProfile["10.0.0.11"])
	}
}

// TestTranslateRefusesAProfileNothingDefines is the typo case, and the reason the check exists at
// all: an undefined profile is not an error anywhere downstream. The lookup returns a zero value,
// concurrency falls back, and the labels and taints the operator expected simply never exist --
// a configuration that looks applied and is not.
func TestTranslateRefusesAProfileNothingDefines(t *testing.T) {
	model := profiledCluster()
	model.Hosts[0].Profile = "infr"

	_, err := translate(context.Background(), model)
	if err == nil {
		t.Fatal("translate accepted a profile the configuration does not define")
	}
	for _, want := range []string{"infr", "10.0.0.21", "control, general, infra"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

// TestTranslateAllowsAProfileWithNoProfilesBlock pins the other half of that check. A `profile`
// with no profiles map is a legitimate configuration: it still names the
// node-role.kubernetes.io/<profile> label the LabelNodes phase writes, and still groups hosts for
// per-profile concurrency. Refusing it would break that for no gain.
func TestTranslateAllowsAProfileWithNoProfilesBlock(t *testing.T) {
	model := threeHosts()
	model.Hosts[1].Profile = "control"

	cfg, err := translate(context.Background(), model)
	if err != nil {
		t.Fatalf("translate refused a profile with no profiles block: %v", err)
	}
	if cfg.Spec.Hosts[0].Profile != "control" {
		t.Errorf("the host's profile is %q", cfg.Spec.Hosts[0].Profile)
	}
	if len(cfg.Spec.Config.Profiles) != 0 {
		t.Errorf("profiles were invented: %+v", cfg.Spec.Config.Profiles)
	}
}

// TestTranslateCarriesHostOverrides covers the per-host half, which is what the Ansible fleet
// example sets in host_vars: this node's own labels and taints, its environment, its ports, and
// the bastion it is reached through.
func TestTranslateCarriesHostOverrides(t *testing.T) {
	model := threeHosts()
	model.Hosts[1].PrivateInterface = "ens224"
	model.Hosts[1].Environment = map[string]string{"NO_PROXY": "10.0.0.0/8"}
	model.Hosts[1].NodeLabels = map[string]string{"adrp.xyz/purpose-infra": "true"}
	model.Hosts[1].NodeTaints = []string{"adrp.xyz/infra=true:NoSchedule"}
	model.Hosts[1].Ports = []portModel{
		{
			Port:     "2049",
			Protocol: "udp",
		},
	}
	model.Hosts[1].Bastion = &bastionModel{
		Address: "10.0.0.1",
		User:    "jump",
		KeyPath: "/srv/staging/keys/bastion_ed25519",
	}

	cfg, err := translate(context.Background(), model)
	if err != nil {
		t.Fatalf("translate reported %v", err)
	}

	host := cfg.Spec.Hosts[0]
	if host.PrivateInterface != "ens224" {
		t.Errorf("the private interface is %q", host.PrivateInterface)
	}
	if host.Environment["NO_PROXY"] != "10.0.0.0/8" {
		t.Errorf("the environment is %+v", host.Environment)
	}
	if host.Engine.NodeLabels["adrp.xyz/purpose-infra"] != "true" {
		t.Errorf("the node labels are %+v", host.Engine.NodeLabels)
	}
	if len(host.Engine.NodeTaints) != 1 {
		t.Errorf("the node taints are %+v", host.Engine.NodeTaints)
	}
	if len(host.Host.Ports) != 1 || host.Host.Ports[0].Protocol != "udp" {
		t.Errorf("the ports are %+v", host.Host.Ports)
	}

	bastion := host.ConnectionConfig.SSH.Bastion
	if bastion == nil {
		t.Fatal("the bastion did not reach the document")
	}
	if bastion.Address != "10.0.0.1" || bastion.User != "jump" {
		t.Errorf("the bastion is %+v", bastion)
	}
	// rig's defaults are written here for the same reason they are written for a host: its YAML
	// tags carry no omitempty, so a port of zero would be written out and fail the schema.
	if bastion.Port != defaultSSHPort {
		t.Errorf("the bastion's port is %d, want the default %d", bastion.Port, defaultSSHPort)
	}
}

// TestTranslateRefusesIncompleteOverrides covers the two fields the API requires and a
// configuration can omit, where the failure downstream would be a rule or a port written to a
// host that means nothing.
func TestTranslateRefusesIncompleteOverrides(t *testing.T) {
	cases := []struct {
		name  string
		model func(clusterModel) clusterModel
		wants string
	}{
		{
			name: "a port with no port",
			model: func(m clusterModel) clusterModel {
				m.Hosts[0].Ports = []portModel{{Protocol: "tcp"}}
				return m
			},
			wants: "no port",
		},
		{
			name: "a firewall rule with no action",
			model: func(m clusterModel) clusterModel {
				m.Hosts[0].FirewallRules = []firewallRuleModel{{Name: "allow-metrics", Port: "9100"}}
				return m
			},
			wants: "no action",
		},
		{
			name: "a bastion with no address",
			model: func(m clusterModel) clusterModel {
				m.Hosts[0].Bastion = &bastionModel{User: "jump"}
				return m
			},
			wants: "bastion with no address",
		},
		{
			name: "a profile port with no port",
			model: func(m clusterModel) clusterModel {
				m.Profiles = map[string]profileModel{"control": {Ports: []portModel{{Protocol: "tcp"}}}}
				return m
			},
			wants: "profile control",
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

// TestTranslatePopulatesValues ensures YAML values are parsed and assigned to Spec.Config.Values.
func TestTranslatePopulatesValues(t *testing.T) {
	model := threeHosts()
	model.Values = `
cilium:
  enabled: true
  ipam:
    mode: kubernetes
`

	cfg, err := translate(context.Background(), model)
	if err != nil {
		t.Fatalf("translate reported %v", err)
	}

	values := cfg.Spec.Config.Values
	if values == nil {
		t.Fatal("Spec.Config.Values is nil, want parsed values")
	}
	cilium, ok := values["cilium"].(map[string]any)
	if !ok {
		t.Fatalf("cilium is a %T, want map[string]any", values["cilium"])
	}
	if cilium["enabled"] != true {
		t.Errorf("cilium.enabled = %v, want true", cilium["enabled"])
	}
}
