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

// Package tofuprovider implements the OpenTofu provider that drives cargoship.
//
// It is a package of this module rather than a module of its own, and what makes that free for the
// CLI is that the linker never loads a package the import graph does not reach: nothing here is
// reachable from cmd/cargoship, which TestCargoshipDoesNotDependOnTheProvider holds. See
// docs/agent/choice-tofu-provider-layout.md, and docs/agent/choice-tofu-secrets.md for the rules
// the schema honours.
package tofuprovider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/internal/clustercfg"
	goyaml "github.com/goccy/go-yaml"
	rig "github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/protocol/ssh"
)

const (
	apiVersion   = "zarf.dev/v1alpha1"
	documentKind = "ZarfCluster"

	// defaultSSHUser and defaultSSHPort are rig's own defaults, written here for the reason
	// internal/ansibleinv writes them: rig applies them when it loads a document, but its YAML
	// tags carry no omitempty, so a config left unset is written out as an empty user and port
	// zero -- and a document saying port 0 fails its own schema.
	defaultSSHUser = "root"
	defaultSSHPort = 22
	// defaultProtocol is what a port entry means when it names only a port. The API requires a
	// protocol, and tcp is what every port a cluster opens uses.
	defaultProtocol = "tcp"
)

// hostRoles are the roles a host block may carry, which are the roles the cluster API defines.
// They are listed rather than derived so the schema description and the error naming them cannot
// drift from each other.
var hostRoles = []string{
	// keep-sorted start
	cluster.RoleController,
	cluster.RoleControllerWorker,
	cluster.RoleSingle,
	cluster.RoleWorker,
	// keep-sorted end
}

// hostModel is one host block of a cargoship_cluster or cargoship_cluster_facts.
//
// There is no attribute for key material, and there will not be: a private key in the resource is
// a private key in the state file. See rule 1 of docs/agent/choice-tofu-secrets.md.
type hostModel struct {
	// Address is how cargoship reaches the host over SSH.
	Address string
	// User is the SSH user, rig's default when empty.
	User string
	// Port is the SSH port, rig's default when zero.
	Port int
	// KeyPath is the path to the private key on the machine running tofu. The key itself never
	// enters the configuration or the state.
	KeyPath string
	// Role is the host's role in the cluster: controller or worker.
	Role string
	// Profile selects a profile from the cluster's profile map, and doubles as the
	// node-role.kubernetes.io/<profile> label the LabelNodes phase writes.
	Profile string
	// Hostname is the name the host is known by when it has not been visited yet.
	Hostname string
	// PrivateAddress overrides the private address the facts phase would discover.
	PrivateAddress string
	// PrivateInterface overrides the private interface the facts phase would discover, for a
	// node with more than one.
	PrivateInterface string
	// Environment are environment variables cargoship sets on the host.
	Environment map[string]string
	// NodeLabels and NodeTaints are this host's own, which replace whatever its profile sets
	// rather than merging with them.
	NodeLabels map[string]string
	NodeTaints []string
	// Ports are the ports opened on this host, replacing its profile's.
	Ports []portModel
	// FirewallRules are this host's own firewall rules. Unlike the fields above, a host's rules
	// are unioned with its profile's rather than replacing them -- see ZarfFirewallConfig.Merge.
	FirewallRules []firewallRuleModel
	// Bastion is the jump host this host is reached through, when it is not reachable directly.
	Bastion *bastionModel
}

// clusterModel is everything a translation needs that is not a host.
type clusterModel struct {
	// Name becomes metadata.name, and the kubeconfig context.
	Name string
	// LoadBalancer is the address clients use to reach the control plane.
	LoadBalancer string
	// Values is the raw YAML string overriding the package's values.
	Values string
	// Profiles maps a profile name to the overrides a host selecting it receives. A profile is
	// how a fleet says "every infra node is tainted this way" once rather than per host.
	Profiles map[string]profileModel
	// Hosts are the fleet, in the order the resource handed them over: by map key, so the first
	// controller -- the leader -- is the controller whose key sorts first.
	Hosts []hostModel
}

// profileModel is one entry of the profiles map.
//
// It is the same shape as a host's own overrides plus a concurrency, because that is what a
// profile is: the overrides a host would otherwise repeat, and how wide to act on the hosts
// sharing them. A host's own values replace a profile's rather than merging into them, which is
// the behaviour cluster.ZarfHostConfig.Merge and ZarfHostEngine.Merge implement.
type profileModel struct {
	// NodeLabels are the Kubernetes node labels applied to a host selecting this profile.
	NodeLabels map[string]string
	// NodeTaints are the Kubernetes node taints applied to a host selecting this profile.
	NodeTaints []string
	// Ports are the ports opened on a host selecting this profile.
	Ports []portModel
	// FirewallRules are the firewall rules applied to a host selecting this profile.
	FirewallRules []firewallRuleModel
	// Concurrency limits how many hosts sharing this profile cargoship acts on at once, as a
	// count ("1") or a percentage of those hosts ("25%").
	Concurrency string
}

// portModel is one port cargoship opens on a host.
type portModel struct {
	Port     string
	Protocol string
}

// firewallRuleModel is one backend-neutral firewall rule. Every match field is optional, and an
// omitted one means "any"; the backends translate a rule into their own dialect.
type firewallRuleModel struct {
	Name        string
	Action      string
	Direction   string
	Source      string
	Destination string
	Ingress     string
	Egress      string
	Port        string
	Protocol    string
}

// bastionModel is the jump host a host is reached through. Cargoship opens its own SSH
// connections, so a bastion has to be stated here rather than inherited from an SSH client
// configuration.
type bastionModel struct {
	Address string
	User    string
	Port    int
	KeyPath string
}

// translate builds a ZarfCluster from a resource or data source model.
//
// It renders the document and reads it back through clustercfg.Parse rather than returning the
// struct it just built. That costs a marshal and a parse, and buys the thing a provider most needs:
// the configuration an operator wrote in HCL is held to exactly the contract a hand-written
// inventory file is held to, by the same code, so a translation mistake is reported as the field it
// landed in rather than as a phase failing against a live host some minutes into an apply.
//
// Host order is load-bearing. ConfigureEngine makes the first controller the leader, so the
// controllers are emitted first, each group in configuration order. A configuration that lists its
// intended leader first gets it.
func translate(ctx context.Context, model clusterModel) (*cluster.ZarfCluster, error) {
	if model.Name == "" {
		return nil, fmt.Errorf("name is required: it becomes metadata.name, and the kubeconfig context")
	}
	if model.LoadBalancer == "" {
		return nil, fmt.Errorf("load_balancer is required: it is the address clients use to reach the control plane")
	}
	if len(model.Hosts) == 0 {
		return nil, fmt.Errorf("at least one host block is required")
	}

	built := make(cluster.ZarfHosts, 0, len(model.Hosts))
	for i := range model.Hosts {
		host, err := hostFrom(model.Hosts[i])
		if err != nil {
			return nil, err
		}
		built = append(built, host)
	}

	// Controllers first, each in configuration order: ConfigureEngine makes the first controller
	// the leader, so a configuration that lists its intended leader first gets it. Everything
	// else keeps its order too, which is what makes a worker batch reproducible.
	hosts := make(cluster.ZarfHosts, 0, len(built))
	for _, host := range built {
		if host.IsController() {
			hosts = append(hosts, host)
		}
	}
	for _, host := range built {
		if !host.IsController() {
			hosts = append(hosts, host)
		}
	}

	profiles, err := profilesFrom(model.Profiles)
	if err != nil {
		return nil, err
	}
	if err := checkProfiles(model); err != nil {
		return nil, err
	}

	var values map[string]any
	if strings.TrimSpace(model.Values) != "" {
		if err := goyaml.Unmarshal([]byte(model.Values), &values); err != nil {
			return nil, fmt.Errorf("unable to parse values YAML: %w", err)
		}
	}

	out := &cluster.ZarfCluster{
		APIVersion: apiVersion,
		Kind:       documentKind,
		Metadata:   cluster.ZarfClusterMetadata{Name: model.Name},
		Spec: cluster.ZarfClusterSpec{
			Config: cluster.ZarfClusterConfig{
				LoadBalancer: model.LoadBalancer,
				Profiles:     profiles,
				Values:       values,
			},
			Hosts: hosts,
		},
	}

	// goccy/go-yaml rather than gopkg.in/yaml.v3, for the reason internal/ansibleinv uses it: it
	// honours the json tags these API types carry, and `Metadata` is tagged json:"-" because it
	// holds runtime state including a func field. yaml.v3 reads yaml tags only, so it walks into
	// that field and panics on the func.
	encoded, err := goyaml.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("unable to encode the generated inventory: %w", err)
	}
	parsed, err := clustercfg.Parse(ctx, encoded)
	if err != nil {
		return nil, fmt.Errorf("the configuration does not describe a valid cluster: %w", err)
	}
	return &parsed, nil
}

// hostFrom builds one host, refusing a role the cluster API does not define.
func hostFrom(model hostModel) (*cluster.ZarfHost, error) {
	if model.Address == "" {
		return nil, fmt.Errorf("every host block needs an address")
	}
	switch model.Role {
	case cluster.RoleController, cluster.RoleControllerWorker, cluster.RoleSingle, cluster.RoleWorker:
	case "":
		return nil, fmt.Errorf("host %s has no role: one of %s", model.Address, strings.Join(hostRoles, ", "))
	default:
		return nil, fmt.Errorf("host %s has role %q, want one of %s",
			model.Address, model.Role, strings.Join(hostRoles, ", "))
	}

	sshCfg := &ssh.Config{
		Address: model.Address,
		User:    model.User,
		Port:    model.Port,
	}
	if sshCfg.User == "" {
		sshCfg.User = defaultSSHUser
	}
	if sshCfg.Port == 0 {
		sshCfg.Port = defaultSSHPort
	}
	if model.KeyPath != "" {
		keyPath := model.KeyPath
		sshCfg.KeyPath = &keyPath
	}

	if model.Bastion != nil {
		if model.Bastion.Address == "" {
			return nil, fmt.Errorf("host %s has a bastion with no address", model.Address)
		}
		bastion := &ssh.Config{
			Address: model.Bastion.Address,
			User:    model.Bastion.User,
			Port:    model.Bastion.Port,
		}
		if bastion.User == "" {
			bastion.User = defaultSSHUser
		}
		if bastion.Port == 0 {
			bastion.Port = defaultSSHPort
		}
		if model.Bastion.KeyPath != "" {
			keyPath := model.Bastion.KeyPath
			bastion.KeyPath = &keyPath
		}
		sshCfg.Bastion = bastion
	}

	ports, err := portsFrom(model.Ports)
	if err != nil {
		return nil, fmt.Errorf("host %s: %w", model.Address, err)
	}
	rules, err := rulesFrom(model.FirewallRules)
	if err != nil {
		return nil, fmt.Errorf("host %s: %w", model.Address, err)
	}

	return &cluster.ZarfHost{
		Role:             model.Role,
		Profile:          model.Profile,
		Hostname:         model.Hostname,
		PrivateAddress:   model.PrivateAddress,
		PrivateInterface: model.PrivateInterface,
		Environment:      model.Environment,
		Host: cluster.ZarfHostConfig{
			Ports:    ports,
			Firewall: cluster.ZarfFirewallConfig{Rules: rules},
		},
		Engine: cluster.ZarfHostEngine{
			NodeLabels: model.NodeLabels,
			NodeTaints: model.NodeTaints,
		},
		ClientWithConfig: rig.ClientWithConfig{
			ConnectionConfig: rig.CompositeConfig{SSH: sshCfg},
		},
	}, nil
}

// profilesFrom renders the profiles map into the cluster document.
func profilesFrom(models map[string]profileModel) (map[string]cluster.ZarfClusterProfiles, error) {
	if len(models) == 0 {
		return nil, nil
	}

	profiles := make(map[string]cluster.ZarfClusterProfiles, len(models))
	for name, model := range models {
		ports, err := portsFrom(model.Ports)
		if err != nil {
			return nil, fmt.Errorf("profile %s: %w", name, err)
		}
		rules, err := rulesFrom(model.FirewallRules)
		if err != nil {
			return nil, fmt.Errorf("profile %s: %w", name, err)
		}
		profiles[name] = cluster.ZarfClusterProfiles{
			Host: cluster.ZarfHostConfig{
				Ports:    ports,
				Firewall: cluster.ZarfFirewallConfig{Rules: rules},
			},
			Engine: cluster.ZarfHostEngine{
				NodeLabels: model.NodeLabels,
				NodeTaints: model.NodeTaints,
			},
			Concurrency: model.Concurrency,
		}
	}
	return profiles, nil
}

// checkProfiles refuses a host selecting a profile the configuration does not define.
//
// Only when the configuration defines profiles at all: a `profile` with no profiles block is a
// legitimate configuration -- it still names the node-role.kubernetes.io label the LabelNodes
// phase writes, and still groups hosts for per-profile concurrency. What is worth refusing is the
// typo, where a profiles block exists and a host names something not in it, because the taints
// and labels that profile would have applied then silently never exist.
func checkProfiles(model clusterModel) error {
	if len(model.Profiles) == 0 {
		return nil
	}

	defined := make([]string, 0, len(model.Profiles))
	for name := range model.Profiles {
		defined = append(defined, name)
	}
	sort.Strings(defined)

	for _, host := range model.Hosts {
		if host.Profile == "" {
			continue
		}
		if _, ok := model.Profiles[host.Profile]; !ok {
			return fmt.Errorf("host %s selects the profile %q, which this configuration does not define: one of %s",
				host.Address, host.Profile, strings.Join(defined, ", "))
		}
	}
	return nil
}

// portsFrom renders the ports a host or profile opens, refusing one that names neither.
func portsFrom(models []portModel) ([]cluster.ZarfHostPort, error) {
	if len(models) == 0 {
		return nil, nil
	}
	ports := make([]cluster.ZarfHostPort, 0, len(models))
	for _, model := range models {
		if model.Port == "" {
			return nil, fmt.Errorf("a port entry has no port")
		}
		protocol := model.Protocol
		if protocol == "" {
			protocol = defaultProtocol
		}
		ports = append(ports, cluster.ZarfHostPort{Port: model.Port, Protocol: protocol})
	}
	return ports, nil
}

// rulesFrom renders firewall rules. Action is the one field the API requires, and a rule without
// it would be written to a host as a rule that matches traffic and does nothing with it.
func rulesFrom(models []firewallRuleModel) ([]cluster.ZarfFirewallRule, error) {
	if len(models) == 0 {
		return nil, nil
	}
	rules := make([]cluster.ZarfFirewallRule, 0, len(models))
	for _, model := range models {
		if model.Action == "" {
			return nil, fmt.Errorf("the firewall rule %q has no action: one of allow, deny or reject", model.Name)
		}
		rules = append(rules, cluster.ZarfFirewallRule{
			Name:        model.Name,
			Action:      model.Action,
			Direction:   model.Direction,
			Source:      model.Source,
			Destination: model.Destination,
			Ingress:     model.Ingress,
			Egress:      model.Egress,
			Port:        model.Port,
			Protocol:    model.Protocol,
		})
	}
	return rules, nil
}
