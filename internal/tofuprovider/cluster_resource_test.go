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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// clusterModelFor is the configuration a practitioner would write, as the framework would decode
// it: one controller and one worker, keyed so the controller is not the first key either.
func clusterModelFor() clusterResourceModel {
	return clusterResourceModel{
		Name:              types.StringValue("bubbles"),
		LoadBalancer:      types.StringValue("10.0.0.10"),
		Package:           types.StringValue("/srv/staging/k3s-v1.33.4.tar.zst"),
		ModifyHosts:       types.BoolValue(true),
		WorkerConcurrency: types.StringValue("25%"),
		Timeout:           types.StringValue("20m"),
		Hosts: map[string]factsHost{
			"worker": {
				Address: types.StringValue("10.0.0.21"),
				Role:    types.StringValue(cluster.RoleWorker),
			},
			"controller": {
				Address: types.StringValue("10.0.0.11"),
				Role:    types.StringValue(cluster.RoleController),
				KeyPath: types.StringValue("/home/operator/.ssh/id_ed25519"),
			},
		},
	}
}

// TestClusterModelOfOrdersAndCarriesHosts holds the resource's half of the translation boundary.
// It is the same assertion the data source's mapping gets, because the two models are separate
// structs the framework decodes into and a field added to one is easy to forget in the other.
func TestClusterModelOfOrdersAndCarriesHosts(t *testing.T) {
	cfg, err := translate(context.Background(), clusterModelOf(clusterModelFor()))
	if err != nil {
		t.Fatalf("translate reported %v", err)
	}

	if len(cfg.Spec.Hosts) != 2 {
		t.Fatalf("the document holds %d hosts, want 2", len(cfg.Spec.Hosts))
	}
	if !cfg.Spec.Hosts[0].IsController() {
		t.Error("the controller was not ordered first, so the wrong host would become the leader")
	}
	key := cfg.Spec.Hosts[0].ConnectionConfig.SSH.KeyPath
	if key == nil || *key != "/home/operator/.ssh/id_ed25519" {
		t.Errorf("the controller's key path did not reach the document: %v", key)
	}
}

// TestConvergePassesEveryOptionToTheApply is what catches an attribute added to the schema and
// wired to nothing. The schema is the contract an operator writes against, and an attribute that
// reaches no action is one that silently does nothing.
func TestConvergePassesEveryOptionToTheApply(t *testing.T) {
	model := clusterModelFor()
	model.ModifyFirewall = types.BoolValue(true)
	model.LabelNodes = types.BoolValue(true)
	model.AllowUnmanagedNodes = types.BoolValue(true)
	model.AllowDowngrade = types.BoolValue(true)
	model.ExportKubeconfig = types.BoolValue(true)

	fake := &fakeConverger{}
	cfg, err := translate(context.Background(), clusterModelOf(model))
	if err != nil {
		t.Fatalf("translate reported %v", err)
	}
	timeout, diag := durationOf(model.Timeout, "timeout")
	if diag != nil {
		t.Fatalf("durationOf reported %v", diag)
	}

	if _, err := fake.Apply(context.Background(), cfg, applyOptions{
		Package:             model.Package.ValueString(),
		ModifyHosts:         model.ModifyHosts.ValueBool(),
		ModifyFirewall:      model.ModifyFirewall.ValueBool(),
		LabelNodes:          model.LabelNodes.ValueBool(),
		WorkerConcurrent:    model.WorkerConcurrency.ValueString(),
		AllowUnmanagedNodes: model.AllowUnmanagedNodes.ValueBool(),
		AllowDowngrade:      model.AllowDowngrade.ValueBool(),
		Timeout:             timeout,
		Kubeconfig:          model.ExportKubeconfig.ValueBool(),
	}); err != nil {
		t.Fatalf("Apply reported %v", err)
	}

	want := applyOptions{
		Package:             "/srv/staging/k3s-v1.33.4.tar.zst",
		ModifyHosts:         true,
		ModifyFirewall:      true,
		LabelNodes:          true,
		WorkerConcurrent:    "25%",
		AllowUnmanagedNodes: true,
		AllowDowngrade:      true,
		Timeout:             20 * time.Minute,
		Kubeconfig:          true,
	}
	if fake.applied == nil || *fake.applied != want {
		t.Errorf("the apply received %+v, want %+v", fake.applied, want)
	}
}

// TestDurationOf covers the one attribute shape an operator can get wrong without the schema
// catching it: a duration the framework accepts as a string and Go refuses.
func TestDurationOf(t *testing.T) {
	cases := []struct {
		name    string
		value   types.String
		want    time.Duration
		wantErr bool
	}{
		{
			name:  "unset",
			value: types.StringNull(),
			want:  0,
		},
		{
			name:  "a go duration",
			value: types.StringValue("20m"),
			want:  20 * time.Minute,
		},
		{
			name:  "whitespace is trimmed",
			value: types.StringValue(" 1h "),
			want:  time.Hour,
		},
		{
			name:    "a bare number is not a duration",
			value:   types.StringValue("20"),
			wantErr: true,
		},
		{
			name:    "minutes spelled out",
			value:   types.StringValue("20 minutes"),
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, diag := durationOf(tc.value, "timeout")
			if tc.wantErr {
				if diag == nil {
					t.Fatalf("durationOf accepted %q", tc.value.ValueString())
				}
				if !strings.Contains(diag.Detail(), "timeout") {
					t.Errorf("the diagnostic did not name the attribute: %s", diag.Detail())
				}
				return
			}
			if diag != nil {
				t.Fatalf("durationOf reported %v", diag)
			}
			if got != tc.want {
				t.Errorf("durationOf was %s, want %s", got, tc.want)
			}
		})
	}
}

// TestFillUnknownLeavesNothingUnknown is what makes a failed apply reportable. State cannot hold
// an unknown value, so a run that failed before producing a computed value would make writing
// state fail too -- and then the only thing reported would be the framework complaining about
// unknowns, with the real error and the facts both lost.
func TestFillUnknownLeavesNothingUnknown(t *testing.T) {
	model := clusterModelFor()
	model.ID = types.StringUnknown()
	model.Distro = types.StringUnknown()
	model.EngineVersion = types.StringUnknown()
	model.Kubeconfig = types.StringUnknown()

	fillUnknown(&model)

	for name, value := range map[string]types.String{
		"id":             model.ID,
		"distro":         model.Distro,
		"engine_version": model.EngineVersion,
		"kubeconfig":     model.Kubeconfig,
	} {
		if value.IsUnknown() {
			t.Errorf("%s is still unknown, so the state could not be written", name)
		}
	}
	if model.ID.ValueString() != "bubbles" {
		t.Errorf("id was %q, want the cluster's name", model.ID.ValueString())
	}
}

// TestApplyResultStillCarriesFactsOnFailure pins the converger's contract from the resource's side:
// a failed apply returns facts, and the resource writes them. Without it the next plan reads an
// empty state and decides nothing is installed, which would re-bootstrap a live cluster.
func TestApplyResultStillCarriesFactsOnFailure(t *testing.T) {
	fake := &fakeConverger{
		applyErr: errors.New("initialize controllers: context deadline exceeded"),
		facts: []hostFacts{
			{Address: "10.0.0.11", EngineVersion: "v1.33.4+k3s1"},
		},
		result: applyResult{DistroID: "k3s", EngineVersion: "v1.33.4+k3s1"},
	}

	cfg, err := translate(context.Background(), clusterModelOf(clusterModelFor()))
	if err != nil {
		t.Fatalf("translate reported %v", err)
	}

	result, err := fake.Apply(context.Background(), cfg, applyOptions{Package: "p"})
	if err == nil {
		t.Fatal("the fake reported no error")
	}
	if len(result.Facts) != 1 {
		t.Fatalf("the failed apply returned %d facts, want the ones it gathered", len(result.Facts))
	}
	if result.DistroID != "k3s" {
		t.Errorf("the failed apply lost the engine it had already resolved: %+v", result)
	}
}

// TestReadReadsTheEngineFromThePackageWhenStateHasNone covers the fallback: a state with no
// engine recorded -- imported, or created by a run that failed before it wrote its result -- has
// nothing for a refresh to target, so the package is asked and the answer is written back.
func TestReadReadsTheEngineFromThePackageWhenStateHasNone(t *testing.T) {
	fake := &fakeConverger{
		pkgDistro: "rke2",
		facts: []hostFacts{
			{Address: "10.0.0.11", EngineVersion: "v1.31.0+rke2r1"},
		},
	}
	r := &clusterResource{converger: fake}

	model := clusterModelFor()
	model.Distro = types.StringValue("") // state has empty distro

	state := stateFor(t, model)
	var resp resource.ReadResponse
	resp.State = state

	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read reported errors: %v", resp.Diagnostics)
	}

	if fake.distroID != "rke2" {
		t.Errorf("Refresh was called with distro %q, want %q", fake.distroID, "rke2")
	}

	var updated clusterResourceModel
	resp.State.Get(context.Background(), &updated)
	if updated.Distro.ValueString() != "rke2" {
		t.Errorf("Read did not update state distro, got %q, want %q", updated.Distro.ValueString(), "rke2")
	}
}

// TestReadFallsBackToDistroInStateWhenPackageUnavailable verifies that Read still has an engine to
// refresh against when the package cannot be read at all.
func TestReadFallsBackToDistroInStateWhenPackageUnavailable(t *testing.T) {
	fake := &fakeConverger{
		pkgDistroErr: errors.New("cannot inspect package"),
		facts: []hostFacts{
			{Address: "10.0.0.11", EngineVersion: "v1.33.4+k3s1"},
		},
	}
	r := &clusterResource{converger: fake}

	model := clusterModelFor()
	model.Distro = types.StringValue("k3s")

	state := stateFor(t, model)
	var resp resource.ReadResponse
	resp.State = state

	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read reported errors: %v", resp.Diagnostics)
	}

	if fake.distroID != "k3s" {
		t.Errorf("Refresh was called with distro %q, want %q", fake.distroID, "k3s")
	}
}

// TestReadDoesNotOpenThePackageWhenStateHasTheEngine is a cost test, not a correctness one.
// distro.Load decompresses the whole archive and hashes every file in it, and Read runs on every
// refresh, so asking the package for something state already holds would put that on every plan.
func TestReadDoesNotOpenThePackageWhenStateHasTheEngine(t *testing.T) {
	fake := &fakeConverger{
		pkgDistro: "rke2",
		facts: []hostFacts{
			{Address: "10.0.0.11", EngineVersion: "v1.33.4+k3s1"},
		},
	}
	r := &clusterResource{converger: fake}

	model := clusterModelFor()
	model.Distro = types.StringValue("k3s")
	model.Package = types.StringValue("./k3s.tar.zst")

	state := stateFor(t, model)
	var resp resource.ReadResponse
	resp.State = state

	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read reported errors: %v", resp.Diagnostics)
	}

	if fake.pkgCalls != 0 {
		t.Errorf("the package was opened %d times during a refresh that had the engine in state", fake.pkgCalls)
	}
	if fake.distroID != "k3s" {
		t.Errorf("Refresh was called with distro %q, want %q", fake.distroID, "k3s")
	}
}

// TestTeardownNeedsTheRecordedDistro covers the destroy path's one hard requirement: a reset loads
// no package, so the engine's identity has to come from state. Tearing down without it would mean
// guessing which engine to remove.
func TestTeardownNeedsTheRecordedDistro(t *testing.T) {
	cfg, err := translate(context.Background(), clusterModelOf(clusterModelFor()))
	if err != nil {
		t.Fatalf("translate reported %v", err)
	}

	cargoship := cargoshipConverger{}
	if err := cargoship.Teardown(context.Background(), cfg, teardownOptions{}); err == nil {
		t.Error("a teardown with no recorded engine was accepted")
	}
}

// TestHostBlocksMatchBetweenTheResourceAndTheDataSource is what keeps the two schemas in step. The
// framework gives resources and data sources different schema packages, so the host block is
// written out twice; an attribute added to one and not the other is a configuration that works in
// one place and fails in the other.
func TestHostBlocksMatchBetweenTheResourceAndTheDataSource(t *testing.T) {
	var resourceResp resource.SchemaResponse
	(&clusterResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resourceResp)

	source, ok := newClusterFactsDataSource().(*clusterFactsDataSource)
	if !ok {
		t.Fatal("newClusterFactsDataSource no longer returns a clusterFactsDataSource")
	}
	var dataResp datasource.SchemaResponse
	source.Schema(context.Background(), datasource.SchemaRequest{}, &dataResp)

	resourceHosts, ok := resourceResp.Schema.Attributes["hosts"].(rschema.MapNestedAttribute)
	if !ok {
		t.Fatalf("the resource's hosts attribute is a %T", resourceResp.Schema.Attributes["hosts"])
	}
	dataHost, ok := dataResp.Schema.Blocks["host"].(dschema.ListNestedBlock)
	if !ok {
		t.Fatalf("the data source's host block is a %T", dataResp.Schema.Blocks["host"])
	}

	for name := range resourceHosts.NestedObject.Attributes {
		if _, found := dataHost.NestedObject.Attributes[name]; !found {
			t.Errorf("the resource's host block has %q and the data source's does not", name)
		}
	}
	for name := range dataHost.NestedObject.Attributes {
		if _, found := resourceHosts.NestedObject.Attributes[name]; !found {
			t.Errorf("the data source's host block has %q and the resource's does not", name)
		}
	}
}

// TestKubeconfigIsSensitive is rule 4 of choice-tofu-secrets, held where it can regress: the
// attribute carries cluster-admin credentials, and an attribute that stops being marked sensitive
// starts appearing in plan output.
func TestKubeconfigIsSensitive(t *testing.T) {
	var resp resource.SchemaResponse
	(&clusterResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)

	kubeconfig, ok := resp.Schema.Attributes["kubeconfig"]
	if !ok {
		t.Fatal("the resource has no kubeconfig attribute")
	}
	if !kubeconfig.IsSensitive() {
		t.Error("kubeconfig is not marked sensitive, so cluster-admin credentials would appear in plan output")
	}
	if !kubeconfig.IsComputed() {
		t.Error("kubeconfig is not computed, which would make it something an operator sets")
	}
}

// resourceSchema is the resource's own schema, which every fixture here is built from.
func resourceSchema(t *testing.T) rschema.Schema {
	t.Helper()

	var resp resource.SchemaResponse
	(&clusterResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("the resource schema is invalid: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// emptyState is the state a create starts from: the right schema holding a null object.
func emptyState(t *testing.T) *tfsdk.State {
	t.Helper()

	s := resourceSchema(t)
	return &tfsdk.State{
		Schema: s,
		Raw:    tftypes.NewValue(s.Type().TerraformType(context.Background()), nil),
	}
}

// withTypedNulls fills in the element types a zero-value collection has not got.
//
// The framework refuses a types.List or types.Map whose element type is unset, and OpenTofu never
// hands one over -- a value it has not computed yet is a typed null. A test fixture that leaves a
// collection at its zero value has to be corrected the same way, and taking the types from the
// schema means a new collection attribute does not have to be remembered here.
func withTypedNulls(t *testing.T, s rschema.Schema, model clusterResourceModel) clusterResourceModel {
	t.Helper()

	v := reflect.ValueOf(&model).Elem()
	for i := range v.NumField() {
		a, ok := s.Attributes[v.Type().Field(i).Tag.Get("tfsdk")]
		if !ok {
			continue
		}
		collection, ok := a.GetType().(attr.TypeWithElementType)
		if !ok {
			continue
		}
		switch field := v.Field(i).Interface().(type) {
		case types.List:
			if field.IsNull() {
				v.Field(i).Set(reflect.ValueOf(types.ListNull(collection.ElementType())))
			}
		case types.Set:
			if field.IsNull() {
				v.Field(i).Set(reflect.ValueOf(types.SetNull(collection.ElementType())))
			}
		case types.Map:
			if field.IsNull() {
				v.Field(i).Set(reflect.ValueOf(types.MapNull(collection.ElementType())))
			}
		}
	}
	return model
}

// stateFor renders a model into a tfsdk.State the way OpenTofu hands one to Read or Delete.
func stateFor(t *testing.T, model clusterResourceModel) tfsdk.State {
	t.Helper()

	state := emptyState(t)
	fixed := withTypedNulls(t, resourceSchema(t), model)
	if diags := state.Set(context.Background(), &fixed); diags.HasError() {
		t.Fatalf("the model does not fit the schema: %v", diags)
	}
	return *state
}

// planFor renders a model into a plan the way OpenTofu hands one to ModifyPlan.
func planFor(t *testing.T, model clusterResourceModel) tfsdk.Plan {
	t.Helper()

	state := emptyState(t)
	plan := tfsdk.Plan{
		Schema: state.Schema,
		Raw:    state.Raw,
	}
	fixed := withTypedNulls(t, resourceSchema(t), model)
	if diags := plan.Set(context.Background(), &fixed); diags.HasError() {
		t.Fatalf("the model does not fit the schema: %v", diags)
	}
	return plan
}

// TestUseStateUnlessPackageChanged covers both halves of the modifier that keeps the package's
// answers out of every plan: the value is held while the package is the same, and released as soon
// as it is not, because a plan that promises the old engine version over a new package fails the
// apply that corrects it.
func TestUseStateUnlessPackageChanged(t *testing.T) {
	prior := clusterModelFor()
	prior.Package = types.StringValue("./k3s-1.33.4.tar.zst")
	prior.EngineVersion = types.StringValue("v1.33.4+k3s1")
	state := stateFor(t, prior)

	for _, tc := range []struct {
		name    string
		pkg     string
		wantSet bool
	}{
		{
			name:    "the same package keeps the recorded version",
			pkg:     "./k3s-1.33.4.tar.zst",
			wantSet: true,
		},
		{
			name:    "a new package leaves it unknown",
			pkg:     "./k3s-1.34.0.tar.zst",
			wantSet: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			planned := prior
			planned.Package = types.StringValue(tc.pkg)
			planned.EngineVersion = types.StringUnknown()

			req := planmodifier.StringRequest{
				Plan:       planFor(t, planned),
				State:      state,
				StateValue: prior.EngineVersion,
				PlanValue:  types.StringUnknown(),
			}
			var resp planmodifier.StringResponse
			resp.PlanValue = req.PlanValue
			useStateUnlessPackageChanged{}.PlanModifyString(context.Background(), req, &resp)

			if tc.wantSet && !resp.PlanValue.Equal(prior.EngineVersion) {
				t.Errorf("the plan value is %s, want the recorded %s", resp.PlanValue, prior.EngineVersion)
			}
			if !tc.wantSet && !resp.PlanValue.IsUnknown() {
				t.Errorf("the plan value is %s, want it left unknown", resp.PlanValue)
			}
		})
	}
}
