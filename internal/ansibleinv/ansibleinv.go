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

// Package ansibleinv translates an Ansible inventory into the ZarfCluster document cargoship
// installs from. Ansible resolves the inventory -- group membership, group_vars, host_vars,
// dynamic inventory plugins, and everything else its own precedence rules cover -- and hands
// the result here already merged. Nothing in this package parses an inventory file, because
// reproducing those rules in Go would reproduce them wrongly.
//
// Ansible supplies the inventory; it does not connect to the fleet. The translated document
// goes to cargoship's own phase pipeline, which opens every SSH connection itself from the
// management node.
package ansibleinv

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/pkg/schema"
	goyaml "github.com/goccy/go-yaml"
)

// apiVersion is the API group and version the generated document declares.
const apiVersion = "zarf.dev/v1alpha1"

// documentKind is the kind the generated document declares.
const documentKind = "ZarfCluster"

// Input is the inventory as Ansible resolved it. Groups and HostVars are Ansible's own `groups`
// and `hostvars`, projected by the action plugin; RoleGroups is the operator's mapping from a
// cargoship role onto the Ansible groups that carry it.
type Input struct {
	// Groups maps an Ansible group name to the hosts in it, in inventory order.
	Groups map[string][]string `json:"groups"`
	// HostVars maps an inventory hostname to the variables Ansible resolved for it.
	HostVars map[string]map[string]any `json:"hostvars"`
	// RoleGroups maps a cargoship role to the Ansible groups whose hosts take that role.
	RoleGroups map[string][]string `json:"roleGroups"`
}

// ClusterMeta is everything the generated document needs that no host carries: the parts of a
// ZarfCluster that describe the cluster rather than a node.
type ClusterMeta struct {
	// Name sets metadata.name, which cargoship uses as the kubeconfig context name.
	Name string `json:"name"`
	// LoadBalancer is the hostname clients use to reach the control plane.
	LoadBalancer string `json:"loadbalancer"`
	// Profiles maps a profile name to the host and engine overrides a host can select.
	Profiles map[string]cluster.ZarfClusterProfiles `json:"profiles,omitempty"`
	// Registries lists the container registries the cluster uses.
	Registries []cluster.ZarfClusterRegistries `json:"registries,omitempty"`
	// Values overrides the values the distro package was built with.
	Values map[string]any `json:"values,omitempty"`
}

// Translate builds a ZarfCluster from a resolved Ansible inventory.
//
// Host order in the result is load-bearing: ConfigureEngine makes the first controller the
// leader, so the leader is the first host of the first group listed under RoleGroups
// ["controller"]. Controllers come before workers, and within a role the groups are walked in
// the order RoleGroups lists them, each group's hosts in Ansible's inventory order.
//
// The result is checked against the embedded inventory schema before it is returned, so a
// translation mistake is reported as the field it landed in rather than as a phase failing
// against a live host some minutes into an apply.
func Translate(in Input, meta ClusterMeta) (*cluster.ZarfCluster, error) {
	if meta.Name == "" {
		return nil, fmt.Errorf("cluster name is required: it becomes metadata.name, and the kubeconfig context")
	}
	if meta.LoadBalancer == "" {
		return nil, fmt.Errorf("cluster loadbalancer is required: it is the address clients use to reach the control plane")
	}

	assignments, err := deriveRoles(in.Groups, in.RoleGroups)
	if err != nil {
		return nil, err
	}

	hosts := make(cluster.ZarfHosts, 0, len(assignments))
	for _, a := range assignments {
		host, err := hostFromVars(a, in.HostVars[a.Host])
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, host)
	}

	out := &cluster.ZarfCluster{
		APIVersion: apiVersion,
		Kind:       documentKind,
		Metadata:   cluster.ZarfClusterMetadata{Name: meta.Name},
		Spec: cluster.ZarfClusterSpec{
			Config: cluster.ZarfClusterConfig{
				LoadBalancer: meta.LoadBalancer,
				Registries:   meta.Registries,
				Profiles:     meta.Profiles,
				Values:       meta.Values,
			},
			Hosts: hosts,
		},
	}

	if err := validate(out); err != nil {
		return nil, err
	}
	return out, nil
}

// validate checks the generated document against the same embedded schema `cargoship validate`
// uses, so the translation is held to the contract an operator's editor holds a hand-written
// inventory to.
// validate checks the generated document against the inventory schema.
//
// It encodes to YAML rather than JSON, because YAML is what gets written and the two are not the
// same document: several of the embedded rig types carry omitempty on their JSON tags and not on
// their YAML tags, so a field absent from the JSON is present and zero in the file. Checking the
// JSON would pass a document the file then fails.
func validate(out *cluster.ZarfCluster) error {
	encoded, err := goyaml.Marshal(out)
	if err != nil {
		return fmt.Errorf("unable to encode the generated inventory: %w", err)
	}
	doc, err := schema.Decode(encoded)
	if err != nil {
		return fmt.Errorf("unable to decode the generated inventory: %w", err)
	}
	validator, err := schema.NewValidatorFor(schema.KindInventory)
	if err != nil {
		return err
	}
	return validator.Validate("the inventory generated from Ansible", doc)
}

// Request is one complete translation: the inventory Ansible resolved, plus the cluster-wide
// settings an inventory has no way to carry. It is what the action plugin sends and what
// `cargoship inventory from-ansible` reads, so the same document that reproduces a translation
// by hand is the one a playbook produced.
type Request struct {
	Input
	// Cluster holds the settings that belong to the cluster rather than to any host.
	Cluster ClusterMeta `json:"cluster"`
}

// Decode reads a Request, refusing a key it does not know. A misspelled key here is a setting
// silently unset, and the two cases this decodes -- a playbook's projection and an operator's
// hand-written file -- both fail quietly without the check.
func Decode(b []byte) (*Request, error) {
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	var req Request
	if err := decoder.Decode(&req); err != nil {
		return nil, fmt.Errorf("unable to read the Ansible inventory: %w", err)
	}
	return &req, nil
}
