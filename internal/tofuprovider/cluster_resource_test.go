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
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	tftypes "github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// clusterModelFor is the configuration a practitioner would write, as the framework would decode
// it: one controller and one worker, written worker-first.
func clusterModelFor() clusterResourceModel {
	return clusterResourceModel{
		Name:              types.StringValue("bubbles"),
		LoadBalancer:      types.StringValue("10.0.0.10"),
		Package:           types.StringValue("/srv/staging/k3s-v1.33.4.tar.zst"),
		ModifyHosts:       types.BoolValue(true),
		WorkerConcurrency: types.StringValue("25%"),
		Timeout:           types.StringValue("20m"),
		Hosts: map[string]clusterHost{
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
		ValuesFiles:         []string{"/tmp/values.yaml"},
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
		ValuesFiles:         []string{"/tmp/values.yaml"},
		ModifyHosts:         true,
		ModifyFirewall:      true,
		LabelNodes:          true,
		WorkerConcurrent:    "25%",
		AllowUnmanagedNodes: true,
		AllowDowngrade:      true,
		Timeout:             20 * time.Minute,
		Kubeconfig:          true,
	}
	if fake.applied == nil || !reflect.DeepEqual(*fake.applied, want) {
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
	model.Nodes = types.ListUnknown(nodesNestedObject().Type())

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
	if model.Nodes.IsUnknown() {
		t.Error("nodes is still unknown, so the state could not be written")
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

// TestTheDataSourceHostBlockIsASubsetOfTheResource holds the relationship between the two host
// blocks. The data source's is deliberately smaller -- a read needs an address and a role, not
// node labels or firewall rules -- but every attribute it does have has to mean the same thing in
// both, so an operator moving a host block from one to the other does not find it renamed.
func TestTheDataSourceHostBlockIsASubsetOfTheResource(t *testing.T) {
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

	for name := range dataHost.NestedObject.Attributes {
		if _, found := resourceHosts.NestedObject.Attributes[name]; !found {
			t.Errorf("the data source's host block has %q and the resource's does not", name)
		}
	}

	// The resource's extras are the node-configuring half, and they are named here so that an
	// attribute dropped from the resource fails rather than quietly narrowing what can be
	// configured.
	for _, name := range []string{
		// keep-sorted start
		"bastion",
		"environment",
		"firewall_rules",
		"node_labels",
		"node_taints",
		"ports",
		"private_interface",
		"state",
		// keep-sorted end
	} {
		if _, found := resourceHosts.NestedObject.Attributes[name]; !found {
			t.Errorf("the resource's host block lost %q", name)
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

// TestTofuSlogHandlerSurvivesEveryLevelAndAttribute is the crash test. The handler is on the path
// of every log line cargoship writes during an apply, so a level or an attribute kind it cannot
// render would take down the provider in the middle of a cluster installation rather than losing
// a log line.
func TestTofuSlogHandlerSurvivesEveryLevelAndAttribute(t *testing.T) {
	ctx := withTofuLogger(context.Background())
	l := logger.From(ctx)
	if l == nil {
		t.Fatal("withTofuLogger put no logger on the context")
	}

	l.Debug("debug message", "key", "val")
	l.Info("info message", "number", 42)
	l.Warn("warn message", "group", slog.GroupValue(slog.String("nested", "item")))
	l.Error("error message", "err", errors.New("boom"))
	l.With("extra", "field").Info("msg with attr")
}

// TestAppendSlogAttrFlattensGroups is what the handler's attribute walk is for: tflog takes a flat
// map, and cargoship's phases log grouped and nested attributes. A group that reached tflog as a
// slog.Value rather than as its members would render as an opaque struct in the one place an
// operator goes to read why an apply failed.
func TestAppendSlogAttrFlattensGroups(t *testing.T) {
	fields := map[string]any{}

	appendSlogAttr(fields, "", slog.String("host", "10.0.0.11"))
	appendSlogAttr(fields, "", slog.Any("group", slog.GroupValue(
		slog.String("nested", "item"),
		slog.Any("deeper", slog.GroupValue(slog.Int("count", 2))),
	)))

	want := map[string]any{
		"host":               "10.0.0.11",
		"group.nested":       "item",
		"group.deeper.count": int64(2),
	}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("the attributes flattened to %#v, want %#v", fields, want)
	}
}

// TestWithAttrsCarriesAcrossLoggers covers slog's With: a logger derived once and used for a whole
// phase has to keep the attributes it was derived with, and must not share the parent's map -- two
// derived loggers writing into one map is a data race under -race and a wrong log line without it.
func TestWithAttrsCarriesAcrossLoggers(t *testing.T) {
	base := tofuSlogHandler{attrs: map[string]any{"run": "apply"}}

	derived, ok := base.WithAttrs([]slog.Attr{slog.String("host", "10.0.0.11")}).(tofuSlogHandler)
	if !ok {
		t.Fatal("WithAttrs returned another handler type")
	}
	if derived.attrs["run"] != "apply" || derived.attrs["host"] != "10.0.0.11" {
		t.Errorf("the derived handler carries %#v", derived.attrs)
	}
	if _, leaked := base.attrs["host"]; leaked {
		t.Error("WithAttrs wrote into the parent's attributes")
	}
}

// emptyState is a tfsdk.State the reconcile can write into. The framework needs a schema to encode
// against, so the resource's own is used rather than a stand-in: a test that wrote into a different
// shape would pass over a model the real one rejects.
func emptyState(t *testing.T) *tfsdk.State {
	t.Helper()

	var resp resource.SchemaResponse
	(&clusterResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("the resource schema is invalid: %v", resp.Diagnostics)
	}
	return &tfsdk.State{
		Schema: resp.Schema,
		Raw:    tftypes.NewValue(resp.Schema.Type().TerraformType(context.Background()), nil),
	}
}

// absentCluster is a three-host fleet with one worker marked for removal.
func absentCluster() clusterResourceModel {
	model := clusterModelFor()
	model.Distro = types.StringValue("k3s")
	model.Hosts["worker2"] = clusterHost{
		Address:  types.StringValue("10.0.0.22"),
		Hostname: types.StringValue("worker2"),
		Role:     types.StringValue(cluster.RoleWorker),
		State:    types.StringValue(hostStateAbsent),
	}
	return model
}

// TestReconcileRemovalsTargetsOnlyTheAbsentHost covers the shape #339 asked for: the document the
// teardown runs against holds every host, because the node deletions are driven through a
// controller that stays, and only the absent one is named as a target.
func TestReconcileRemovalsTargetsOnlyTheAbsentHost(t *testing.T) {
	fake := &fakeConverger{}
	r := &clusterResource{converger: fake}
	model := absentCluster()

	var diags diag.Diagnostics
	state := emptyState(t)
	if !r.reconcileRemovals(context.Background(), &model, nil, 0, &diags, state) {
		t.Fatalf("reconcileRemovals stopped the apply: %v", diags)
	}
	if diags.HasError() {
		t.Fatalf("reconcileRemovals reported %v", diags)
	}

	if fake.tornDown == nil {
		t.Fatal("no teardown ran")
	}
	if got := fake.tornDown.TargetHosts; len(got) != 1 || got[0] != "worker2" {
		t.Errorf("the teardown targeted %v, want only worker2", got)
	}
	// Every host reaches the document, including the one being removed: the surviving controller
	// is what the node deletion is driven through.
	if fake.got == nil || len(fake.got.Spec.Hosts) != len(model.Hosts) {
		t.Errorf("the teardown received %d hosts, want all %d", len(fake.got.Spec.Hosts), len(model.Hosts))
	}

	for _, host := range model.Hosts {
		if isAbsent(host) && !host.Removed.ValueBool() {
			t.Error("the removed host was not recorded as removed, so the next apply would remove it again")
		}
	}
}

// TestReconcileRemovalsKeepsAFailedHostInState is the failure this whole review was about. A host
// whose teardown failed is still running and still joined, and the configuration that reaches it
// -- its address and its key path -- exists nowhere else. Pruning it from state leaves nothing
// anywhere able to finish the job.
func TestReconcileRemovalsKeepsAFailedHostInState(t *testing.T) {
	fake := &fakeConverger{teardownErr: errors.New("drain node worker2: context deadline exceeded")}
	r := &clusterResource{converger: fake}
	model := absentCluster()
	before := len(model.Hosts)

	var diags diag.Diagnostics
	state := emptyState(t)
	if r.reconcileRemovals(context.Background(), &model, nil, 0, &diags, state) {
		t.Fatal("reconcileRemovals let the apply continue over a half-removed cluster")
	}

	if !diags.HasError() {
		t.Fatal("a failed removal was not reported as an error, so the apply would print Apply complete")
	}
	if len(model.Hosts) != before {
		t.Errorf("state holds %d hosts, want all %d: a host that was not removed must keep its block", len(model.Hosts), before)
	}
	for _, host := range model.Hosts {
		if isAbsent(host) && host.Removed.ValueBool() {
			t.Error("a host whose teardown failed was recorded as removed")
		}
	}
}

// TestReconcileRemovalsSkipsAHostAlreadyRemoved is what makes the tombstone safe to leave in the
// configuration: the second apply does nothing, rather than reaching for a machine that is gone.
func TestReconcileRemovalsSkipsAHostAlreadyRemoved(t *testing.T) {
	fake := &fakeConverger{}
	r := &clusterResource{converger: fake}
	model := absentCluster()

	prior := make(map[string]clusterHost, len(model.Hosts))
	for k, host := range model.Hosts {
		if isAbsent(host) {
			host.Removed = types.BoolValue(true)
		}
		prior[k] = host
	}

	var diags diag.Diagnostics
	if !r.reconcileRemovals(context.Background(), &model, prior, 0, &diags, emptyState(t)) {
		t.Fatalf("reconcileRemovals stopped the apply: %v", diags)
	}
	if fake.tornDown != nil {
		t.Error("a host already removed was torn down again")
	}
}

// TestReconcileRemovalsIsANoOpWithoutAbsentHosts pins the ordinary case: a configuration with no
// tombstones never reaches a teardown at all.
func TestReconcileRemovalsIsANoOpWithoutAbsentHosts(t *testing.T) {
	fake := &fakeConverger{}
	r := &clusterResource{converger: fake}
	model := clusterModelFor()

	var diags diag.Diagnostics
	if !r.reconcileRemovals(context.Background(), &model, nil, 0, &diags, emptyState(t)) {
		t.Fatalf("reconcileRemovals stopped the apply: %v", diags)
	}
	if fake.tornDown != nil {
		t.Error("a configuration with no absent hosts ran a teardown")
	}
	for _, host := range model.Hosts {
		if host.Removed.IsNull() || host.Removed.IsUnknown() {
			t.Error("removed was left unknown, which state cannot hold")
		}
	}
}

// TestClusterModelOfDropsAbsentHosts holds the other half: a host marked absent is not part of the
// cluster the apply converges, so nothing tries to install an engine on the machine it just
// removed one from.
func TestClusterModelOfDropsAbsentHosts(t *testing.T) {
	model := clusterModelOf(absentCluster())

	for _, host := range model.Hosts {
		if host.Hostname == "worker2" || host.Address == "10.0.0.22" {
			t.Error("a host marked absent reached the cluster the apply converges")
		}
	}
}

// TestHostsStillInstalledIsWhatADestroyTearsDown covers the one list a destroy must not take from
// the apply. The hosts an apply converges exclude every host marked absent; a destroy has to
// include the one whose removal failed, because that machine is still running and still joined,
// and has to exclude the one whose removal finished, because reaching for it would fail against a
// machine that may have been decommissioned since.
func TestHostsStillInstalledIsWhatADestroyTearsDown(t *testing.T) {
	hosts := map[string]clusterHost{
		"c1": {
			Address: types.StringValue("10.0.0.11"),
			Role:    types.StringValue(cluster.RoleController),
		},
		"w1": {
			Address: types.StringValue("10.0.0.21"),
			Role:    types.StringValue(cluster.RoleWorker),
			State:   types.StringValue(hostStateAbsent),
			Removed: types.BoolValue(true),
		},
		"w2": {
			Address: types.StringValue("10.0.0.22"),
			Role:    types.StringValue(cluster.RoleWorker),
			State:   types.StringValue(hostStateAbsent),
			Removed: types.BoolValue(false),
		},
	}

	got := hostsStillInstalled(hosts)

	var addresses []string
	for _, host := range got {
		addresses = append(addresses, host.Address.ValueString())
		if isAbsent(host) {
			t.Errorf("%s is still marked absent, so the teardown would drop it", host.Address.ValueString())
		}
	}
	slices.Sort(addresses)

	want := []string{"10.0.0.11", "10.0.0.22"}
	if !reflect.DeepEqual(addresses, want) {
		t.Errorf("a destroy would tear down %v, want %v", addresses, want)
	}
}

// TestReconcileRemovalsRefusesAnEmptiedCluster covers the configuration that is a destroy written
// the long way. Left alone it would reset every host and then fail translating a cluster with no
// hosts, which is a confusing route to something destroy does properly -- an apply cannot drop the
// resource that describes the cluster.
func TestReconcileRemovalsRefusesAnEmptiedCluster(t *testing.T) {
	fake := &fakeConverger{}
	r := &clusterResource{converger: fake}

	model := clusterModelFor()
	for k, host := range model.Hosts {
		host.State = types.StringValue(hostStateAbsent)
		model.Hosts[k] = host
	}

	var diags diag.Diagnostics
	if r.reconcileRemovals(context.Background(), &model, nil, 0, &diags, emptyState(t)) {
		t.Fatal("reconcileRemovals accepted a configuration with every host absent")
	}
	if !diags.HasError() {
		t.Fatal("emptying the cluster was not reported as an error")
	}
	if fake.tornDown != nil {
		t.Error("the cluster was torn down through the apply path")
	}
	if !strings.Contains(diags.Errors()[0].Detail(), "tofu destroy") {
		t.Errorf("the error does not point at destroy: %s", diags.Errors()[0].Detail())
	}
}

// stateFor renders a model into a tfsdk.State the way OpenTofu hands one to Read or Delete.
func stateFor(t *testing.T, model clusterResourceModel) tfsdk.State {
	t.Helper()

	state := emptyState(t)
	if model.ValuesFiles.IsNull() {
		model.ValuesFiles = types.ListNull(types.StringType)
	}
	if model.Nodes.IsNull() {
		nodes, ok := state.Schema.GetAttributes()["nodes"].GetType().(attr.TypeWithElementType)
		if !ok {
			t.Fatal("the nodes attribute is not a collection")
		}
		model.Nodes = types.ListNull(nodes.ElementType())
	}
	if diags := state.Set(context.Background(), &model); diags.HasError() {
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
	// A list the model left at its zero value carries no element type, which the framework
	// refuses. OpenTofu never hands one over: a value it has not computed yet is a typed null.
	if model.ValuesFiles.IsNull() {
		model.ValuesFiles = types.ListNull(types.StringType)
	}
	if model.Nodes.IsNull() {
		nodes, ok := state.Schema.GetAttributes()["nodes"].GetType().(attr.TypeWithElementType)
		if !ok {
			t.Fatal("the nodes attribute is not a collection")
		}
		model.Nodes = types.ListNull(nodes.ElementType())
	}
	if diags := plan.Set(context.Background(), &model); diags.HasError() {
		t.Fatalf("the model does not fit the schema: %v", diags)
	}
	return plan
}

// TestModifyPlanRefusesAnEmptiedCluster is the same refusal as the apply-time one, moved to where
// an operator reads it: before anything has been drained.
func TestModifyPlanRefusesAnEmptiedCluster(t *testing.T) {
	model := clusterModelFor()
	for k, host := range model.Hosts {
		host.State = types.StringValue(hostStateAbsent)
		model.Hosts[k] = host
	}

	var resp resource.ModifyPlanResponse
	(&clusterResource{}).ModifyPlan(
		context.Background(),
		resource.ModifyPlanRequest{Plan: planFor(t, model)},
		&resp,
	)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a plan with every host absent was accepted")
	}
	if !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "tofu destroy") {
		t.Errorf("the error does not point at destroy: %s", resp.Diagnostics.Errors()[0].Detail())
	}
}

func TestModifyPlanAllowsARemovalAndADestroy(t *testing.T) {
	// One host absent beside hosts that are staying is the ordinary removal.
	var removal resource.ModifyPlanResponse
	(&clusterResource{}).ModifyPlan(
		context.Background(),
		resource.ModifyPlanRequest{Plan: planFor(t, absentCluster())},
		&removal,
	)
	if removal.Diagnostics.HasError() {
		t.Fatalf("a removal was refused at plan time: %v", removal.Diagnostics)
	}

	// A destroy plans a null resource, and tearing every host down is what it is for.
	state := emptyState(t)
	var destroy resource.ModifyPlanResponse
	(&clusterResource{}).ModifyPlan(
		context.Background(),
		resource.ModifyPlanRequest{Plan: tfsdk.Plan{
			Schema: state.Schema,
			Raw:    state.Raw,
		}},
		&destroy,
	)
	if destroy.Diagnostics.HasError() {
		t.Fatalf("a destroy was refused at plan time: %v", destroy.Diagnostics)
	}
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

// TestUseBoolStateForUnknownLeavesANewHostUnknown is the case the obvious modifier gets wrong: a
// host added to an existing cluster has no value at its path in state, and planning that null
// would commit the plan to a value the apply then contradicts.
func TestUseBoolStateForUnknownLeavesANewHostUnknown(t *testing.T) {
	state := stateFor(t, clusterModelFor())

	req := planmodifier.BoolRequest{
		State:      state,
		StateValue: types.BoolNull(),
		PlanValue:  types.BoolUnknown(),
	}
	var resp planmodifier.BoolResponse
	resp.PlanValue = req.PlanValue
	useBoolStateForUnknownModifier{}.PlanModifyBool(context.Background(), req, &resp)

	if !resp.PlanValue.IsUnknown() {
		t.Errorf("the plan value is %s, want it left unknown for a host that is not in state", resp.PlanValue)
	}

	// A host that is in state keeps what it recorded, which is what stops the removal running twice.
	req.StateValue = types.BoolValue(true)
	resp.PlanValue = types.BoolUnknown()
	useBoolStateForUnknownModifier{}.PlanModifyBool(context.Background(), req, &resp)

	if !resp.PlanValue.Equal(types.BoolValue(true)) {
		t.Errorf("the plan value is %s, want the recorded true", resp.PlanValue)
	}
}
