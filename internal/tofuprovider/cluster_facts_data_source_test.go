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
	"errors"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// fakeConverger answers from a table rather than from a fleet. It is the seam the provider exists
// around: everything below it needs SSH and a cluster, and everything above it -- the state
// mapping and the diagnostics, which is where provider bugs live -- does not.
type fakeConverger struct {
	facts []hostFacts
	err   error

	// got records what the provider asked for, so a test can assert the translation reached it
	// rather than only that the result came back.
	got      *cluster.ZarfCluster
	distroID string
}

func (f *fakeConverger) Refresh(_ context.Context, cfg *cluster.ZarfCluster, distroID string) ([]hostFacts, error) {
	f.got = cfg
	f.distroID = distroID
	return f.facts, f.err
}

// factsModelFor is the configuration a practitioner would write, as the framework would decode it.
func factsModelFor() factsModel {
	return factsModel{
		Name:         types.StringValue("bubbles"),
		LoadBalancer: types.StringValue("10.0.0.10"),
		Distro:       types.StringValue("k3s"),
		Hosts: []factsHost{
			{
				Address: types.StringValue("10.0.0.21"),
				Role:    types.StringValue(cluster.RoleWorker),
			},
			{
				Address: types.StringValue("10.0.0.11"),
				Role:    types.StringValue(cluster.RoleController),
				User:    types.StringValue("operator"),
				Port:    types.Int64Value(2222),
				KeyPath: types.StringValue("/home/operator/.ssh/id_ed25519"),
			},
		},
	}
}

// TestModelOfCarriesEveryHostAttribute holds the boundary between the framework's types and plain
// Go. A field added to the schema and not to this mapping is a configuration an operator writes
// and the provider silently ignores, which no other test would catch.
func TestModelOfCarriesEveryHostAttribute(t *testing.T) {
	model := modelOf(factsModelFor())

	if model.Name != "bubbles" || model.LoadBalancer != "10.0.0.10" {
		t.Errorf("the cluster was %+v", model)
	}
	if len(model.Hosts) != 2 {
		t.Fatalf("the model holds %d hosts, want 2", len(model.Hosts))
	}

	controller := model.Hosts[1]
	want := hostModel{
		Address: "10.0.0.11",
		User:    "operator",
		Port:    2222,
		KeyPath: "/home/operator/.ssh/id_ed25519",
		Role:    cluster.RoleController,
	}
	if controller != want {
		t.Errorf("the controller mapped to %+v, want %+v", controller, want)
	}

	// An unset optional attribute is a null types.String, and reading one through ValueString
	// gives the empty string -- which is what translate takes as "apply the default".
	worker := model.Hosts[0]
	if worker.User != "" || worker.Port != 0 || worker.KeyPath != "" {
		t.Errorf("an unset attribute reached the model as a value: %+v", worker)
	}
}

// TestNodesOfRendersEveryFact is the other half of the same boundary, read the other way: a fact
// gathered and not rendered is one a configuration cannot route on.
func TestNodesOfRendersEveryFact(t *testing.T) {
	nodes := nodesOf([]hostFacts{
		{
			Address:        "10.0.0.11:22",
			Hostname:       "kc0",
			OS:             "ubuntu",
			OSVersion:      "24.04",
			Arch:           "amd64",
			PrivateAddress: "10.0.0.11",
			Role:           cluster.RoleController,
			EngineVersion:  "v1.33.4+k3s1",
		},
	})

	if len(nodes) != 1 {
		t.Fatalf("rendered %d nodes, want 1", len(nodes))
	}
	node := nodes[0]
	for name, got := range map[string]types.String{
		"address":         node.Address,
		"hostname":        node.Hostname,
		"os":              node.OS,
		"os_version":      node.OSVersion,
		"arch":            node.Arch,
		"private_address": node.PrivateAddress,
		"role":            node.Role,
		"engine_version":  node.EngineVersion,
	} {
		if got.IsNull() || got.ValueString() == "" {
			t.Errorf("%s was not rendered", name)
		}
	}
	if node.EngineVersion.ValueString() != "v1.33.4+k3s1" {
		t.Errorf("engine_version was %q", node.EngineVersion.ValueString())
	}
}

// TestFactsReadPassesTheTranslationThrough drives the data source's own logic: the configuration
// becomes a cluster document, the document reaches the converger, and the distro comes with it.
// It needs no SSH, because the converger is the seam.
func TestFactsReadPassesTheTranslationThrough(t *testing.T) {
	fake := &fakeConverger{
		facts: []hostFacts{
			{Address: "10.0.0.11:22", EngineVersion: "v1.33.4+k3s1"},
		},
	}
	source := &clusterFactsDataSource{converger: fake}

	cfg, err := translate(context.Background(), modelOf(factsModelFor()))
	if err != nil {
		t.Fatalf("translate reported %v", err)
	}
	facts, err := source.converger.Refresh(context.Background(), cfg, "k3s")
	if err != nil {
		t.Fatalf("Refresh reported %v", err)
	}

	if fake.distroID != "k3s" {
		t.Errorf("the converger was asked for distro %q", fake.distroID)
	}
	if fake.got == nil || fake.got.Metadata.Name != "bubbles" {
		t.Fatalf("the converger did not receive the translated cluster: %+v", fake.got)
	}
	if !fake.got.Spec.Hosts[0].IsController() {
		t.Error("the controller was not ordered first in the document the converger received")
	}
	if len(facts) != 1 || facts[0].EngineVersion != "v1.33.4+k3s1" {
		t.Errorf("the facts came back as %+v", facts)
	}
}

// TestFactsReadReportsAFailure pins what an unreachable fleet looks like: an error naming the
// cluster, not a partially populated state.
func TestFactsReadReportsAFailure(t *testing.T) {
	fake := &fakeConverger{err: errors.New("dial 10.0.0.11:22: connection refused")}
	source := &clusterFactsDataSource{converger: fake}

	cfg, err := translate(context.Background(), modelOf(factsModelFor()))
	if err != nil {
		t.Fatalf("translate reported %v", err)
	}
	if _, err := source.converger.Refresh(context.Background(), cfg, "k3s"); err == nil {
		t.Fatal("Refresh reported no error")
	}
}

// TestRefreshRefusesADistroItCannotResolve covers the message an operator sees for a typo in
// `distro`, which is the one attribute with a closed set of values and no way to validate it
// against the registry at schema time. It reaches the real converger rather than the fake, and
// fails before any connection is attempted -- which is why it needs no host.
func TestRefreshRefusesADistroItCannotResolve(t *testing.T) {
	cfg, err := translate(context.Background(), modelOf(factsModelFor()))
	if err != nil {
		t.Fatalf("translate reported %v", err)
	}

	cargoship := cargoshipConverger{}
	for _, distroID := range []string{"", "k3ss"} {
		if _, err := cargoship.Refresh(context.Background(), cfg, distroID); err == nil {
			t.Errorf("the distro %q was accepted", distroID)
		}
	}
}
