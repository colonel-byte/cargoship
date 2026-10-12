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
}

// clusterModel is everything a translation needs that is not a host.
type clusterModel struct {
	// Name becomes metadata.name, and the kubeconfig context.
	Name string
	// LoadBalancer is the address clients use to reach the control plane.
	LoadBalancer string
	// Hosts are the fleet, in the order the resource handed them over: by map key, so the first
	// controller -- the leader -- is the controller whose key sorts first.
	Hosts []hostModel
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

	out := &cluster.ZarfCluster{
		APIVersion: apiVersion,
		Kind:       documentKind,
		Metadata:   cluster.ZarfClusterMetadata{Name: model.Name},
		Spec: cluster.ZarfClusterSpec{
			Config: cluster.ZarfClusterConfig{
				LoadBalancer: model.LoadBalancer,
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

	return &cluster.ZarfHost{
		Role:           model.Role,
		Profile:        model.Profile,
		Hostname:       model.Hostname,
		PrivateAddress: model.PrivateAddress,
		ClientWithConfig: rig.ClientWithConfig{
			ConnectionConfig: rig.CompositeConfig{SSH: sshCfg},
		},
	}, nil
}
