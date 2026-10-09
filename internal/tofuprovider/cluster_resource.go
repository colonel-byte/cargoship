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
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// clusterResource is the cluster itself: the configuration describes the fleet, cargoship owns the
// phase ordering, and an apply converges it.
//
// Create and Update are the same call, because `action.NewApply` builds one phase list covering
// install, join and upgrade with each phase gated by its own ShouldRun. There is nothing for the
// resource to branch on, and a resource that branched would be deciding something the phases
// already decide from what the hosts report.
type clusterResource struct {
	converger converger
}

// clusterResource implements the plan-time hook as well as the CRUD interface; the assertion is
// here because a ModifyPlan with a drifted signature is silently never called.
var _ resource.ResourceWithModifyPlan = (*clusterResource)(nil)

func newClusterResource() resource.Resource {
	return &clusterResource{}
}

const (
	// hostStatePresent is a host that belongs to the cluster, which is what a `hosts` entry means
	// when it says nothing.
	hostStatePresent = "present"
	// hostStateAbsent is a host to remove from the cluster: drained, deleted, and its engine
	// uninstalled, while every other host is left running.
	//
	// It is stated rather than inferred from a deleted block, because a deleted block takes the
	// address and the key path with it -- see docs/agent/choice-removed-hosts.md.
	hostStateAbsent = "absent"
)

// clusterResourceModel is the resource's state.
type clusterResourceModel struct {
	Name         types.String `tfsdk:"name"`
	LoadBalancer types.String `tfsdk:"load_balancer"`
	Package      types.String `tfsdk:"package"`

	ModifyHosts         types.Bool   `tfsdk:"modify_hosts"`
	ModifyFirewall      types.Bool   `tfsdk:"modify_firewall"`
	LabelNodes          types.Bool   `tfsdk:"label_nodes"`
	WorkerConcurrency   types.String `tfsdk:"worker_concurrency"`
	AllowUnmanagedNodes types.Bool   `tfsdk:"allow_unmanaged_nodes"`
	AllowDowngrade      types.Bool   `tfsdk:"allow_downgrade"`
	Timeout             types.String `tfsdk:"timeout"`
	ExportKubeconfig    types.Bool   `tfsdk:"export_kubeconfig"`
	RetainOnDestroy     types.Bool   `tfsdk:"retain_on_destroy"`
	NoDrainOnDestroy    types.Bool   `tfsdk:"no_drain_on_destroy"`

	Values      types.String `tfsdk:"values"`
	ValuesFiles types.List   `tfsdk:"values_files"`

	ID            types.String `tfsdk:"id"`
	Distro        types.String `tfsdk:"distro"`
	EngineVersion types.String `tfsdk:"engine_version"`
	Kubeconfig    types.String `tfsdk:"kubeconfig"`

	Profiles map[string]profileAttr `tfsdk:"profiles"`

	Hosts map[string]clusterHost `tfsdk:"hosts"`
	Nodes types.List             `tfsdk:"nodes"`
}

// profileAttr is one entry of the profiles map.
type profileAttr struct {
	NodeLabels    map[string]string `tfsdk:"node_labels"`
	NodeTaints    []string          `tfsdk:"node_taints"`
	Ports         []portAttr        `tfsdk:"ports"`
	FirewallRules []firewallAttr    `tfsdk:"firewall_rules"`
	Concurrency   types.String      `tfsdk:"concurrency"`
}

// clusterHost is one entry of the resource's host map. It is the data source's host block plus everything
// that configures a node rather than reaching it, which is why the two are separate types: a read
// needs an address and a role, and an apply needs the rest.
type clusterHost struct {
	Address        types.String `tfsdk:"address"`
	User           types.String `tfsdk:"user"`
	Port           types.Int64  `tfsdk:"port"`
	KeyPath        types.String `tfsdk:"key_path"`
	Role           types.String `tfsdk:"role"`
	Profile        types.String `tfsdk:"profile"`
	Hostname       types.String `tfsdk:"hostname"`
	PrivateAddress types.String `tfsdk:"private_address"`
	State          types.String `tfsdk:"state"`
	// Removed records that a host marked absent has been torn down. It is what lets the block
	// stay in the configuration afterwards without the removal running again on every apply.
	Removed types.Bool `tfsdk:"removed"`

	PrivateInterface types.String      `tfsdk:"private_interface"`
	Environment      map[string]string `tfsdk:"environment"`
	NodeLabels       map[string]string `tfsdk:"node_labels"`
	NodeTaints       []string          `tfsdk:"node_taints"`
	Ports            []portAttr        `tfsdk:"ports"`
	FirewallRules    []firewallAttr    `tfsdk:"firewall_rules"`
	Bastion          *bastionAttr      `tfsdk:"bastion"`
}

// portAttr is one port a host or profile opens.
type portAttr struct {
	Port     types.String `tfsdk:"port"`
	Protocol types.String `tfsdk:"protocol"`
}

// firewallAttr is one backend-neutral firewall rule.
type firewallAttr struct {
	Name        types.String `tfsdk:"name"`
	Action      types.String `tfsdk:"action"`
	Direction   types.String `tfsdk:"direction"`
	Source      types.String `tfsdk:"source"`
	Destination types.String `tfsdk:"destination"`
	Ingress     types.String `tfsdk:"ingress"`
	Egress      types.String `tfsdk:"egress"`
	Port        types.String `tfsdk:"port"`
	Protocol    types.String `tfsdk:"protocol"`
}

// bastionAttr is the jump host a host is reached through.
type bastionAttr struct {
	Address types.String `tfsdk:"address"`
	User    types.String `tfsdk:"user"`
	Port    types.Int64  `tfsdk:"port"`
	KeyPath types.String `tfsdk:"key_path"`
}

func (r *clusterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster"
}

func (r *clusterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Kubernetes cluster cargoship installs, joins and upgrades. One resource holds every " +
			"host: cargoship takes a cluster-wide lock and starts controllers in a batch of one for quorum, so a " +
			"resource per node would contend on the lock and lose the ordering batching expresses.\n\n" +
			"An apply converges the cluster on the configuration. There is no separate install and upgrade: the engine " +
			"version already on each host is what tells one from the other, and a downgrade is refused rather than " +
			"attempted.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "The cluster's name. It becomes `metadata.name` and the kubeconfig context, and " +
					"changing it replaces the cluster.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"load_balancer": schema.StringAttribute{
				MarkdownDescription: "The address clients use to reach the control plane.",
				Required:            true,
			},
			"package": schema.StringAttribute{
				MarkdownDescription: "The distro package to install: a path to a `.tar.zst` built by " +
					"`cargoship package create`, or an OCI reference. A package reference is resolved when the apply " +
					"runs, so an airgapped management node wants a path.",
				Required: true,
			},

			"modify_hosts": schema.BoolAttribute{
				MarkdownDescription: "Rewrite `/etc/hosts` on every host with the cluster's own nodes.",
				Optional:            true,
			},
			"modify_firewall": schema.BoolAttribute{
				MarkdownDescription: "Rewrite the host firewall (firewalld, ufw or nftables) with the engine's rules.",
				Optional:            true,
			},
			"label_nodes": schema.BoolAttribute{
				MarkdownDescription: "Add the `node-role.kubernetes.io/<profile>` label to each node, from the host's " +
					"`profile`.",
				Optional: true,
			},
			"worker_concurrency": schema.StringAttribute{
				MarkdownDescription: "How many workers are installed or upgraded at a time, as a count (`5`) or a " +
					"percentage of the batch (`25%`).",
				Optional: true,
			},
			"allow_unmanaged_nodes": schema.BoolAttribute{
				MarkdownDescription: "Continue when the cluster holds a node no `hosts` entry accounts for. An apply " +
					"removes nothing, so by default a node left behind by a deleted entry stops the run.",
				Optional: true,
			},
			"allow_downgrade": schema.BoolAttribute{
				MarkdownDescription: "Continue when a host already runs an engine newer than the package carries. " +
					"A downgrade is refused by default, because an engine does not support being moved backwards.",
				Optional: true,
			},
			"timeout": schema.StringAttribute{
				MarkdownDescription: "How long the phases wait for a host to reach the state they want, as a Go " +
					"duration (`20m`). It bounds the retry loops inside a run, not the run itself.",
				Optional: true,
			},
			"export_kubeconfig": schema.BoolAttribute{
				MarkdownDescription: "Return the cluster's admin credentials in the `kubeconfig` attribute. It is " +
					"off by default and should stay off unless something in the configuration consumes it: the " +
					"credentials are cluster-admin, and any computed attribute lands in the state file whether or " +
					"not it is marked sensitive. `cargoship install kube-config` writes a kubeconfig without " +
					"putting one in state.",
				Optional: true,
			},
			"retain_on_destroy": schema.BoolAttribute{
				MarkdownDescription: "Leave the cluster running when the resource is destroyed, dropping it from " +
					"state with a warning instead of resetting it. Destroying resets by default, because that is " +
					"what destroy means everywhere else.",
				Optional: true,
			},
			"no_drain_on_destroy": schema.BoolAttribute{
				MarkdownDescription: "Skip draining each node before it is deleted during a destroy. Faster, and " +
					"it gives workloads no chance to move.",
				Optional: true,
			},
			"values": schema.StringAttribute{
				MarkdownDescription: "Overrides for the values the distro package was built with, as a YAML string " +
					"(e.g. from `file(\"values.yaml\")` or `yamlencode(...)`).",
				Optional: true,
			},
			"values_files": schema.ListAttribute{
				MarkdownDescription: "Paths to YAML values files overriding the values the package ships with. " +
					"Loaded and merged in order, winning over inline `values`.",
				Optional:    true,
				ElementType: types.StringType,
			},

			"id": schema.StringAttribute{
				MarkdownDescription: "The cluster's name, which is its identity here. There is no `import`: the " +
					"`hosts` entries and their key paths are not recoverable from a running cluster.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"distro": schema.StringAttribute{
				MarkdownDescription: "The engine the package carries, read from the package rather than stated twice. " +
					"It is what a later refresh and a destroy need.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					useStateUnlessPackageChanged{},
				},
			},
			"engine_version": schema.StringAttribute{
				MarkdownDescription: "The engine version the package carries.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					useStateUnlessPackageChanged{},
				},
			},
			"kubeconfig": schema.StringAttribute{
				MarkdownDescription: "The cluster's admin credentials, populated only when `export_kubeconfig` is set.",
				Computed:            true,
				Sensitive:           true,
			},
			"profiles": schema.MapNestedAttribute{
				MarkdownDescription: "The profiles a host can select, keyed by name. A profile is how a fleet says " +
					"\"every infra node is tainted this way\" once rather than per host, and it is also where " +
					"per-profile concurrency is set.\n\n" +
					"A host's own `node_labels`, `node_taints` and `ports` **replace** its profile's rather than " +
					"merging with them; firewall rules are the exception and are unioned. A host that selects a " +
					"profile this map does not define is a configuration error, since the labels and taints it " +
					"expected would otherwise silently never exist.",
				Optional:     true,
				NestedObject: profilesNestedObject(),
			},
			"hosts": schema.MapNestedAttribute{
				MarkdownDescription: "The fleet, keyed by hostname or an identifier of your choosing. The key names " +
					"the host when `hostname` is not set, and it is what orders the run: controllers are acted on " +
					"first, and the controller whose key sorts first becomes the leader.",
				Optional: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: clusterHostAttributes(),
				},
			},
			"nodes": schema.ListNestedAttribute{
				MarkdownDescription: "What each host reported during the run, in the order the apply acted on them: " +
					"controllers first.",
				Computed:     true,
				NestedObject: nodesNestedObject(),
			},
		},
	}
}

// Configure takes the converger the provider built.
func (r *clusterResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(converger)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("the resource was configured with a %T rather than something that can reach cargoship; this is a bug in the provider", req.ProviderData),
		)
		return
	}
	r.converger = c
}

// Create converges a cluster that is not in state yet.
func (r *clusterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model clusterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Nothing was ever installed, so a host marked absent at create time is one to leave alone
	// rather than one to tear down. There is no prior state to carry.
	r.converge(ctx, &model, nil, &resp.Diagnostics, &resp.State)
}

// Update is the same call as Create. See the type comment.
func (r *clusterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var model clusterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The prior state says which removals have already happened. Without it a host left marked
	// absent would be torn down again on every apply, which means reaching a machine that is
	// gone -- and failing once it has been decommissioned.
	var prior clusterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.converge(ctx, &model, prior.Hosts, &resp.Diagnostics, &resp.State)
}

// Read refreshes what the hosts report, without changing them.
//
// An unreachable host is a diagnostic rather than a removal. Calling RemoveResource on a transient
// SSH failure would make the next apply re-bootstrap a running cluster, which is the worst thing
// this resource could do.
func (r *clusterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var model clusterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg, err := translate(ctx, clusterModelOf(model))
	if err != nil {
		resp.Diagnostics.AddError("Invalid cluster configuration", err.Error())
		return
	}

	distroID := r.resolveDistro(ctx, &model)
	facts, err := r.converger.Refresh(ctx, cfg, distroID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to refresh the cluster",
			fmt.Sprintf("cargoship could not read %s: %s", model.Name.ValueString(), err),
		)
		return
	}

	if distroID != "" && model.Distro.ValueString() == "" {
		model.Distro = types.StringValue(distroID)
	}

	nodesList, nodesDiags := types.ListValueFrom(ctx, nodesNestedObject().Type(), nodesOf(facts))
	resp.Diagnostics.Append(nodesDiags...)
	model.Nodes = nodesList
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

// Delete resets the cluster, unless it was told to leave it running.
func (r *clusterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var model clusterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if model.RetainOnDestroy.ValueBool() {
		resp.Diagnostics.AddWarning(
			"The cluster was left running",
			fmt.Sprintf("%s was dropped from state with retain_on_destroy set, so its engine, its data and its "+
				"nodes are all still there. Run `cargoship install reset` against it to tear it down.",
				model.Name.ValueString()),
		)
		return
	}

	// A destroy tears down whatever is still out there, which is not the same as the hosts the
	// apply converges. A host marked absent whose removal failed is still running and still
	// joined, and clusterModelOf drops every absent host -- so taking that list would walk past
	// exactly the machine a failed removal left behind, and report a destroy that finished.
	//
	// A host already removed is left out for the opposite reason: it is gone, and reaching for it
	// would fail against a machine that may since have been decommissioned.
	teardownModel := model
	teardownModel.Hosts = hostsStillInstalled(model.Hosts)
	if len(teardownModel.Hosts) == 0 {
		tflog.Info(ctx, "nothing to reset: every host was already removed", map[string]any{
			"cluster": model.Name.ValueString(),
		})
		return
	}

	cfg, err := translate(ctx, clusterModelOf(teardownModel))
	if err != nil {
		resp.Diagnostics.AddError("Invalid cluster configuration", err.Error())
		return
	}

	timeout, diag := durationOf(model.Timeout, "timeout")
	if diag != nil {
		resp.Diagnostics.Append(diag)
		return
	}

	tflog.Info(ctx, "resetting the cluster", map[string]any{
		"cluster": model.Name.ValueString(),
		"hosts":   len(teardownModel.Hosts),
	})
	distroID := r.resolveDistro(ctx, &model)
	if err := r.converger.Teardown(ctx, cfg, teardownOptions{
		DistroID:         distroID,
		WorkerConcurrent: model.WorkerConcurrency.ValueString(),
		NoDrain:          model.NoDrainOnDestroy.ValueBool(),
		Timeout:          timeout,
	}); err != nil {
		// The state is left in place. A reset that failed part way through has removed the engine
		// from some hosts and not others, and dropping the resource would leave nothing pointing
		// at the half that is left.
		resp.Diagnostics.AddError(
			"Unable to reset the cluster",
			fmt.Sprintf("cargoship could not tear down %s, and it is left in state so the remaining hosts are still "+
				"described: %s", model.Name.ValueString(), err),
		)
	}
}

// converge is the body Create and Update share.
//
// State is written whether or not the apply succeeded, and that is the whole point of the shape:
// an apply that failed has already changed hosts, so a resource that returned without writing
// would leave the next plan deciding nothing is installed.
// ModifyPlan reports the configuration an apply cannot converge, while the operator is still
// reading a diff rather than watching hosts being drained.
//
// Only the emptied cluster is checked here. It is the one refusal that is visible from the
// configuration alone: every other one -- a controller that has to survive the run, a target
// naming no host -- depends on which engines are actually running, which the provider learns by
// connecting, and connecting during a plan is what a plan is not allowed to do.
func (r *clusterResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// A destroy plans a null resource, and destroying every host is exactly what it is for.
	if req.Plan.Raw.IsNull() {
		return
	}

	var model clusterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if len(model.Hosts) == 0 {
		return
	}
	for _, host := range model.Hosts {
		// An unknown state is a value that comes from somewhere else in the configuration; it
		// could resolve either way, so the plan says nothing about it.
		if host.State.IsUnknown() || !isAbsent(host) {
			return
		}
	}
	resp.Diagnostics.Append(emptiedClusterDiagnostic(model.Name.ValueString()))
}

// emptiedClusterDiagnostic is the refusal for a configuration whose every host is marked absent.
//
// It is raised from the plan and again during the apply, because the two are reached
// independently: a plan file applied later holds the configuration that was planned, and the
// apply is where the hosts already recorded as removed are known.
func emptiedClusterDiagnostic(name string) diag.Diagnostic {
	return diag.NewErrorDiagnostic(
		"Every host is marked absent",
		fmt.Sprintf("%s would have no hosts left, and an apply cannot remove the resource that describes it. "+
			"Run `tofu destroy` against it instead, or set retain_on_destroy if the machines are going away "+
			"with it.", name),
	)
}

func (r *clusterResource) converge(
	ctx context.Context,
	model *clusterResourceModel,
	prior map[string]clusterHost,
	diags *diag.Diagnostics,
	state *tfsdk.State,
) {
	timeout, diag := durationOf(model.Timeout, "timeout")
	if diag != nil {
		diags.Append(diag)
		return
	}

	// Removals come first, and a removal that failed stops the run: converging on top of a
	// half-removed cluster would report success over a node that is still joined.
	if !r.reconcileRemovals(ctx, model, prior, timeout, diags, state) {
		return
	}

	cfg, err := translate(ctx, clusterModelOf(*model))
	if err != nil {
		diags.AddError("Invalid cluster configuration", err.Error())
		return
	}

	tflog.Info(ctx, "converging the cluster", map[string]any{
		"cluster": model.Name.ValueString(),
		"package": model.Package.ValueString(),
		"hosts":   len(model.Hosts),
	})

	var valuesFiles []string
	if !model.ValuesFiles.IsNull() && !model.ValuesFiles.IsUnknown() {
		diags.Append(model.ValuesFiles.ElementsAs(ctx, &valuesFiles, false)...)
		if diags.HasError() {
			return
		}
	}

	result, runErr := r.converger.Apply(ctx, cfg, applyOptions{
		Package:             model.Package.ValueString(),
		ValuesFiles:         valuesFiles,
		ModifyHosts:         model.ModifyHosts.ValueBool(),
		ModifyFirewall:      model.ModifyFirewall.ValueBool(),
		LabelNodes:          model.LabelNodes.ValueBool(),
		WorkerConcurrent:    model.WorkerConcurrency.ValueString(),
		AllowUnmanagedNodes: model.AllowUnmanagedNodes.ValueBool(),
		AllowDowngrade:      model.AllowDowngrade.ValueBool(),
		Timeout:             timeout,
		Kubeconfig:          model.ExportKubeconfig.ValueBool(),
	})

	model.ID = model.Name
	if result.DistroID != "" {
		model.Distro = types.StringValue(result.DistroID)
	}
	if result.EngineVersion != "" {
		model.EngineVersion = types.StringValue(result.EngineVersion)
	}
	nodesList, nodesDiags := types.ListValueFrom(ctx, nodesNestedObject().Type(), nodesOf(result.Facts))
	diags.Append(nodesDiags...)
	model.Nodes = nodesList
	model.Kubeconfig = types.StringValue(string(result.Kubeconfig))

	// Unknown values are illegal in state, and a failed run leaves whatever it never reached
	// unknown. Filling them in is what lets the error below be reported alongside a state that
	// still describes the fleet.
	fillUnknown(model)

	diags.Append(state.Set(ctx, model)...)

	if runErr != nil {
		diags.AddError(
			"The apply did not finish",
			fmt.Sprintf("cargoship could not converge %s: %s\n\nWhat it reached before failing has been written to "+
				"state, so the next plan sees the hosts as they are rather than as empty.",
				model.Name.ValueString(), runErr),
		)
	}
}

// reconcileRemovals tears down the hosts marked `state = "absent"` that have not been torn down
// already, and reports whether the apply should go on.
//
// Three things about it are deliberate, and each of them is a failure mode that was easy to write
// by accident.
//
// A host stays in state either way. On success it keeps its block with `removed = true`, which is
// what stops the next apply from tearing it down again -- a removal that repeated would have to
// reach a machine that is gone, and would fail once it was decommissioned. On failure it keeps its
// block with `removed` false, so the configuration that still describes how to reach it survives:
// a host pruned from state after a failed teardown is a running, still-joined node that nothing
// anywhere holds an address or a key for.
//
// A failure is a diagnostic rather than a warning. tflog output is invisible without TF_LOG, so a
// warning here meant an apply that printed "Apply complete!" over a cluster that still held the
// node.
//
// And a failure stops the apply. The convergence that follows would otherwise run against a
// cluster left half-removed -- drained and deleted but not uninstalled, or the reverse.
func (r *clusterResource) reconcileRemovals(
	ctx context.Context,
	model *clusterResourceModel,
	prior map[string]clusterHost,
	timeout time.Duration,
	diags *diag.Diagnostics,
	state *tfsdk.State,
) bool {
	removed := removedHosts(prior)

	keys := make([]string, 0, len(model.Hosts))
	for k := range model.Hosts {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	var targets []string
	var pending []string
	for _, k := range keys {
		host := model.Hosts[k]
		if !isAbsent(host) {
			continue
		}
		// Already gone: keep the record, do nothing. This is the difference between a tombstone
		// an operator can leave in place and one they have to remember to delete.
		target := hostKey(k, host)
		if removed[target] {
			host.Removed = types.BoolValue(true)
			model.Hosts[k] = host
			continue
		}
		targets = append(targets, target)
		pending = append(pending, k)
	}

	// Every host marked absent is a teardown wearing a removal's clothes: the apply would reset
	// the whole cluster and then fail translating a cluster with no hosts in it, which is a
	// confusing way to arrive at something `tofu destroy` does properly -- including dropping the
	// resource afterwards, which an apply cannot do.
	if len(targets) > 0 && len(targets) == len(model.Hosts) {
		diags.Append(emptiedClusterDiagnostic(model.Name.ValueString()))
		return false
	}

	// Nothing to remove, or nothing that has not been removed already.
	if len(targets) == 0 {
		for k, h := range model.Hosts {
			if h.Removed.IsUnknown() || h.Removed.IsNull() {
				h.Removed = types.BoolValue(false)
				model.Hosts[k] = h
			}
		}
		return true
	}

	// A teardown needs the hosts that are staying as well as the ones going: the node deletions
	// are driven through a controller that survives, which is the whole point of #339. So the
	// document holds every host, and the target list is what says which of them to remove.
	full := *model
	full.Hosts = make(map[string]clusterHost, len(model.Hosts))
	for k, host := range model.Hosts {
		host.State = types.StringValue(hostStatePresent)
		full.Hosts[k] = host
	}

	cfg, err := translate(ctx, clusterModelOf(full))
	if err != nil {
		diags.AddError(
			"Invalid cluster configuration",
			fmt.Sprintf("the hosts marked absent could not be removed, because the configuration does not describe a "+
				"valid cluster: %s", err),
		)
		return false
	}

	tflog.Info(ctx, "removing hosts marked absent", map[string]any{
		"cluster": model.Name.ValueString(),
		"targets": targets,
	})

	distroID := r.resolveDistro(ctx, model)
	if err := r.converger.Teardown(ctx, cfg, teardownOptions{
		DistroID:         distroID,
		TargetHosts:      targets,
		WorkerConcurrent: model.WorkerConcurrency.ValueString(),
		NoDrain:          model.NoDrainOnDestroy.ValueBool(),
		Timeout:          timeout,
	}); err != nil {
		for _, k := range pending {
			h := model.Hosts[k]
			h.Removed = types.BoolValue(false)
			model.Hosts[k] = h
		}
		fillUnknown(model)
		diags.Append(state.Set(ctx, model)...)
		diags.AddError(
			"The hosts marked absent were not removed",
			fmt.Sprintf("cargoship could not remove %s from %s: %s\n\nThey are still described in state, so the "+
				"configuration that reaches them is intact and the removal can be retried. The rest of the apply did "+
				"not run, because converging on top of a half-removed cluster would report success over a node that "+
				"is still joined.",
				strings.Join(targets, ", "), model.Name.ValueString(), err),
		)
		return false
	}

	for _, k := range pending {
		h := model.Hosts[k]
		h.Removed = types.BoolValue(true)
		model.Hosts[k] = h
	}
	for k, h := range model.Hosts {
		if h.Removed.IsUnknown() || h.Removed.IsNull() {
			h.Removed = types.BoolValue(false)
			model.Hosts[k] = h
		}
	}
	return true
}

// hostsStillInstalled are the hosts a destroy has to tear down: everything the cluster still holds,
// including a host marked absent whose removal did not finish, and excluding one whose removal did.
func hostsStillInstalled(hosts map[string]clusterHost) map[string]clusterHost {
	out := make(map[string]clusterHost, len(hosts))
	for k, host := range hosts {
		if isAbsent(host) && host.Removed.ValueBool() {
			continue
		}
		host.State = types.StringValue(hostStatePresent)
		out[k] = host
	}
	return out
}

// isAbsent reports whether a host entry asks to be removed from the cluster.
func isAbsent(host clusterHost) bool {
	return strings.EqualFold(host.State.ValueString(), hostStateAbsent)
}

// resolveDistro returns the engine to act on: what state recorded, and failing that what the
// package carries.
//
// State is asked first because reading the package is not cheap -- distro.Load decompresses the
// whole archive into a temporary directory and hashes every file in it to compute the manifest --
// and Read runs on every refresh, which is every plan. The package is the fallback for the state
// that has no engine recorded: a resource imported rather than created, or a create that failed
// after the hosts were reached but before the apply wrote its result.
func (r *clusterResource) resolveDistro(ctx context.Context, model *clusterResourceModel) string {
	if distroID := model.Distro.ValueString(); distroID != "" {
		return distroID
	}
	pkg := model.Package.ValueString()
	if pkg == "" || r.converger == nil {
		return ""
	}
	distroID, err := r.converger.DistroFromPackage(ctx, pkg)
	if err != nil {
		tflog.Warn(ctx, "could not read the engine from the package", map[string]any{
			"package": pkg,
			"error":   err.Error(),
		})
		return ""
	}
	return distroID
}

func hostKey(key string, host clusterHost) string {
	if name := host.Hostname.ValueString(); name != "" {
		return name
	}
	if key != "" {
		return key
	}
	return host.Address.ValueString()
}

// removedHosts indexes the prior state by the hosts it records as already removed.
func removedHosts(prior map[string]clusterHost) map[string]bool {
	out := make(map[string]bool, len(prior))
	for k, host := range prior {
		if host.Removed.ValueBool() {
			out[k] = true
			if name := host.Hostname.ValueString(); name != "" {
				out[name] = true
			}
			if addr := host.Address.ValueString(); addr != "" {
				out[addr] = true
			}
		}
	}
	return out
}

// fillUnknown replaces any computed value the run never produced with a known zero, since state
// cannot hold an unknown.
func fillUnknown(model *clusterResourceModel) {
	if model.Distro.IsUnknown() {
		model.Distro = types.StringValue("")
	}
	if model.EngineVersion.IsUnknown() {
		model.EngineVersion = types.StringValue("")
	}
	if model.Kubeconfig.IsUnknown() {
		model.Kubeconfig = types.StringValue("")
	}
	if model.ID.IsUnknown() {
		model.ID = model.Name
	}
	if model.Nodes.IsUnknown() {
		model.Nodes = types.ListNull(nodesNestedObject().Type())
	}
}

// useStateUnlessPackageChanged keeps a computed attribute's prior value in the plan, so a plan
// does not read "(known after apply)" on every attribute the package decides -- except when the
// package itself is changing, where the prior value is exactly what will not survive the apply.
//
// stringplanmodifier.UseStateForUnknown is the usual answer and is wrong here: it holds the old
// engine version across a package upgrade, and an apply that returns a different value than the
// plan promised fails with "provider produced inconsistent result after apply".
type useStateUnlessPackageChanged struct{}

func (useStateUnlessPackageChanged) Description(_ context.Context) string {
	return "Keeps the value recorded in state unless the package is changing."
}

func (useStateUnlessPackageChanged) MarkdownDescription(_ context.Context) string {
	return "Keeps the value recorded in state unless the `package` is changing."
}

func (useStateUnlessPackageChanged) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || !req.PlanValue.IsUnknown() || req.StateValue.IsNull() {
		return
	}

	var planned, prior types.String
	if req.Plan.GetAttribute(ctx, path.Root("package"), &planned).HasError() {
		return
	}
	if req.State.GetAttribute(ctx, path.Root("package"), &prior).HasError() {
		return
	}
	if !planned.Equal(prior) {
		return
	}
	resp.PlanValue = req.StateValue
}

type useBoolStateForUnknownModifier struct{}

func (useBoolStateForUnknownModifier) Description(_ context.Context) string {
	return "Once set, the value of this attribute in state will not change unless modified."
}

func (useBoolStateForUnknownModifier) MarkdownDescription(_ context.Context) string {
	return "Once set, the value of this attribute in state will not change unless modified."
}

func (useBoolStateForUnknownModifier) PlanModifyBool(_ context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	if req.State.Raw.IsNull() || !req.PlanValue.IsUnknown() || req.ConfigValue.IsUnknown() {
		return
	}
	// A host added to an existing cluster has no value at this path in state, and planning the
	// null it would return is worse than planning unknown: the apply records false, and a
	// computed attribute whose planned value does not survive the apply fails the run.
	if req.StateValue.IsNull() {
		return
	}
	resp.PlanValue = req.StateValue
}

// durationOf reads a Go duration attribute, or reports why it could not.
func durationOf(value types.String, attribute string) (time.Duration, diag.Diagnostic) {
	text := strings.TrimSpace(value.ValueString())
	if text == "" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(text)
	if err != nil {
		return 0, diag.NewAttributeErrorDiagnostic(
			path.Root(attribute),
			"Invalid duration",
			fmt.Sprintf("%s %q is not a Go duration such as 20m or 1h: %s", attribute, text, err),
		)
	}
	return parsed, nil
}

// hostBlockAttributes and nodesNestedObject are the host block and the gathered-facts list as the
// resource schema package spells them.
//
// They are written out here rather than shared with the data source because the framework gives
// resources and data sources different schema packages: the attribute types are distinct even
// though the attributes are the same. What keeps the two in step is
// TestHostBlocksMatchBetweenTheResourceAndTheDataSource, which holds the attribute names against
// each other.
func hostBlockAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"address": schema.StringAttribute{
			MarkdownDescription: "The address or hostname cargoship opens an SSH connection to.",
			Required:            true,
		},
		"role": schema.StringAttribute{
			MarkdownDescription: "The host's role: one of `" + strings.Join(hostRoles, "`, `") + "`.",
			Required:            true,
		},
		"user": schema.StringAttribute{
			MarkdownDescription: "The SSH user. Defaults to `root`.",
			Optional:            true,
		},
		"port": schema.Int64Attribute{
			MarkdownDescription: "The SSH port. Defaults to 22.",
			Optional:            true,
		},
		"key_path": schema.StringAttribute{
			MarkdownDescription: "Path to the SSH private key on the machine running OpenTofu. There is no " +
				"attribute for key material, and there will not be: a key in the configuration is a key in the " +
				"state file.",
			Optional: true,
		},
		"profile": schema.StringAttribute{
			MarkdownDescription: "The profile this host selects from the cluster's profile map, which also becomes " +
				"its `node-role.kubernetes.io/<profile>` label when `label_nodes` is set.",
			Optional: true,
		},
		"hostname": schema.StringAttribute{
			MarkdownDescription: "The name the host is known by before it has been visited.",
			Optional:            true,
		},
		"private_address": schema.StringAttribute{
			MarkdownDescription: "Overrides the private address the facts phase would discover.",
			Optional:            true,
		},
	}
}

// profilesNestedObject is one entry of the profiles map.
// clusterHostAttributes is the resource's host block: everything needed to reach a host, plus
// everything that configures the node it becomes.
//
// It is hostBlockAttributes plus the node-configuring half. The data source keeps the smaller set
// on purpose -- a read needs an address and a role and nothing else -- and
// TestTheDataSourceHostBlockIsASubsetOfTheResource holds the relationship between the two.
func clusterHostAttributes() map[string]schema.Attribute {
	attributes := hostBlockAttributes()

	attributes["state"] = schema.StringAttribute{
		MarkdownDescription: "The lifecycle state of this host: `present` (the default) or `absent`. Marking a host " +
			"`absent` drains it, deletes it from the cluster and uninstalls its engine on the next apply, while " +
			"every other host keeps running.\n\n" +
			"The block stays where it is afterwards -- `removed` records that the work is done, so later applies " +
			"leave the machine alone and never try to reach one that has been decommissioned. Delete the block " +
			"whenever it suits; nothing depends on it going.\n\n" +
			"Removal is stated rather than inferred from a deleted block, because deleting the block takes the " +
			"address and the key path with it: see " +
			"[choice-removed-hosts](https://github.com/colonel-byte/cargoship/blob/main/docs/agent/choice-removed-hosts.md).",
		Optional: true,
	}
	attributes["removed"] = schema.BoolAttribute{
		MarkdownDescription: "Whether this host has been removed from the cluster. Computed: it becomes true once a " +
			"host marked `absent` has been torn down, and that is what stops the removal from running again.",
		Computed: true,
		PlanModifiers: []planmodifier.Bool{
			useBoolStateForUnknownModifier{},
		},
	}

	attributes["private_interface"] = schema.StringAttribute{
		MarkdownDescription: "Overrides the private interface the facts phase would discover, for a node with more " +
			"than one.",
		Optional: true,
	}
	attributes["environment"] = schema.MapAttribute{
		MarkdownDescription: "Environment variables cargoship sets on the host, for a proxy or a no-proxy list.",
		Optional:            true,
		ElementType:         types.StringType,
	}
	attributes["node_labels"] = schema.MapAttribute{
		MarkdownDescription: "Kubernetes node labels for this host. They **replace** the labels of the profile the " +
			"host selects rather than merging with them.",
		Optional:    true,
		ElementType: types.StringType,
	}
	attributes["node_taints"] = schema.ListAttribute{
		MarkdownDescription: "Kubernetes node taints for this host, as `key=value:Effect`. They replace the " +
			"profile's rather than merging with them.",
		Optional:    true,
		ElementType: types.StringType,
	}
	attributes["ports"] = schema.ListNestedAttribute{
		MarkdownDescription: "Ports opened on this host, replacing the profile's.",
		Optional:            true,
		NestedObject:        portsNestedObject(),
	}
	attributes["firewall_rules"] = schema.ListNestedAttribute{
		MarkdownDescription: "Firewall rules for this host. Unlike the fields above, these are unioned with the " +
			"profile's rather than replacing them.",
		Optional:     true,
		NestedObject: firewallNestedObject(),
	}
	attributes["bastion"] = schema.SingleNestedAttribute{
		MarkdownDescription: "The jump host this host is reached through. Cargoship opens its own SSH connections, " +
			"so a bastion has to be stated here rather than inherited from an SSH client configuration.",
		Optional: true,
		Attributes: map[string]schema.Attribute{
			"address": schema.StringAttribute{
				MarkdownDescription: "The bastion's address.",
				Required:            true,
			},
			"user": schema.StringAttribute{
				MarkdownDescription: "The SSH user on the bastion. Defaults to `root`.",
				Optional:            true,
			},
			"port": schema.Int64Attribute{
				MarkdownDescription: "The SSH port on the bastion. Defaults to 22.",
				Optional:            true,
			},
			"key_path": schema.StringAttribute{
				MarkdownDescription: "Path to the private key for the bastion, on the machine running OpenTofu.",
				Optional:            true,
			},
		},
	}
	return attributes
}

func profilesNestedObject() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"node_labels": schema.MapAttribute{
				MarkdownDescription: "Kubernetes node labels applied to a host selecting this profile.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"node_taints": schema.ListAttribute{
				MarkdownDescription: "Kubernetes node taints applied to a host selecting this profile, as " +
					"`key=value:Effect`.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"ports": schema.ListNestedAttribute{
				MarkdownDescription: "Ports opened on a host selecting this profile.",
				Optional:            true,
				NestedObject:        portsNestedObject(),
			},
			"firewall_rules": schema.ListNestedAttribute{
				MarkdownDescription: "Firewall rules applied to a host selecting this profile. A host's own rules " +
					"are unioned with these rather than replacing them.",
				Optional:     true,
				NestedObject: firewallNestedObject(),
			},
			"concurrency": schema.StringAttribute{
				MarkdownDescription: "How many hosts sharing this profile cargoship acts on at once -- draining, " +
					"upgrading, initializing, uninstalling -- as a count (`1`) or a percentage of those hosts " +
					"(`25%`). Empty falls back to the phase's own concurrency.",
				Optional: true,
			},
		},
	}
}

// portsNestedObject is one port entry, shared by a host and a profile.
func portsNestedObject() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"port": schema.StringAttribute{
				MarkdownDescription: "The port number or inclusive range, as a string: `6443`, `30000-32767`.",
				Required:            true,
			},
			"protocol": schema.StringAttribute{
				MarkdownDescription: "`tcp` or `udp`. Defaults to `tcp`.",
				Optional:            true,
			},
		},
	}
}

// firewallNestedObject is one backend-neutral firewall rule. Every match field is optional and an
// omitted one means "any"; the backends translate a rule into firewalld, ufw or nftables, so not
// every combination is expressible everywhere.
func firewallNestedObject() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"action": schema.StringAttribute{
				MarkdownDescription: "What happens to matching traffic: `allow`, `deny` or `reject`. It is the one " +
					"field a rule cannot omit.",
				Required: true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Names the rule, and names the artifacts cargoship writes on the node for it, " +
					"so it has to be unique within a host. One is generated when it is empty.",
				Optional: true,
			},
			"direction": schema.StringAttribute{
				MarkdownDescription: "`in`, `out` or `forward`. Defaults to `in`.",
				Optional:            true,
			},
			"source":      schema.StringAttribute{MarkdownDescription: "The address or CIDR the traffic comes from.", Optional: true},
			"destination": schema.StringAttribute{MarkdownDescription: "The address or CIDR the traffic goes to.", Optional: true},
			"ingress": schema.StringAttribute{
				MarkdownDescription: "Where forward traffic enters: a zone on firewalld hosts, an interface on ufw " +
					"hosts. Forward rules only.",
				Optional: true,
			},
			"egress": schema.StringAttribute{
				MarkdownDescription: "Where forward traffic leaves, read the same way as `ingress`. Forward rules only.",
				Optional:            true,
			},
			"port":     schema.StringAttribute{MarkdownDescription: "The port or inclusive range the rule matches.", Optional: true},
			"protocol": schema.StringAttribute{MarkdownDescription: "The protocol the rule matches.", Optional: true},
		},
	}
}

func nodesNestedObject() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"address":         schema.StringAttribute{Computed: true, MarkdownDescription: "The address cargoship connected to."},
			"hostname":        schema.StringAttribute{Computed: true, MarkdownDescription: "The hostname the host reports."},
			"os":              schema.StringAttribute{Computed: true, MarkdownDescription: "The `ID` from the host's os-release."},
			"os_version":      schema.StringAttribute{Computed: true, MarkdownDescription: "The `VERSION_ID` from the host's os-release."},
			"arch":            schema.StringAttribute{Computed: true, MarkdownDescription: "The CPU architecture the host reports."},
			"private_address": schema.StringAttribute{Computed: true, MarkdownDescription: "The private address cargoship discovered, or the one the host block declared."},
			"role":            schema.StringAttribute{Computed: true, MarkdownDescription: "The role the host block gave it."},
			"engine_version": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The engine version running on the host, or `v0.0.0` for a host with no engine " +
					"installed. This is the value a plan-time downgrade check compares a package against.",
			},
		},
	}
}

// clusterModelOf is the translation boundary for the resource, the counterpart of modelOf.
func clusterModelOf(model clusterResourceModel) clusterModel {
	keys := make([]string, 0, len(model.Hosts))
	for k := range model.Hosts {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	hosts := make([]hostModel, 0, len(model.Hosts))
	for _, k := range keys {
		host := model.Hosts[k]
		if strings.EqualFold(host.State.ValueString(), "absent") {
			continue
		}
		hostname := host.Hostname.ValueString()
		if hostname == "" {
			hostname = k
		}
		mapped := hostModel{
			Address:          host.Address.ValueString(),
			User:             host.User.ValueString(),
			Port:             int(host.Port.ValueInt64()),
			KeyPath:          host.KeyPath.ValueString(),
			Role:             host.Role.ValueString(),
			Profile:          host.Profile.ValueString(),
			Hostname:         hostname,
			PrivateAddress:   host.PrivateAddress.ValueString(),
			PrivateInterface: host.PrivateInterface.ValueString(),
			Environment:      host.Environment,
			NodeLabels:       host.NodeLabels,
			NodeTaints:       host.NodeTaints,
			Ports:            portsOf(host.Ports),
			FirewallRules:    rulesOf(host.FirewallRules),
		}
		if host.Bastion != nil {
			mapped.Bastion = &bastionModel{
				Address: host.Bastion.Address.ValueString(),
				User:    host.Bastion.User.ValueString(),
				Port:    int(host.Bastion.Port.ValueInt64()),
				KeyPath: host.Bastion.KeyPath.ValueString(),
			}
		}
		hosts = append(hosts, mapped)
	}

	var profiles map[string]profileModel
	if len(model.Profiles) > 0 {
		profiles = make(map[string]profileModel, len(model.Profiles))
		for name, profile := range model.Profiles {
			profiles[name] = profileModel{
				NodeLabels:    profile.NodeLabels,
				NodeTaints:    profile.NodeTaints,
				Ports:         portsOf(profile.Ports),
				FirewallRules: rulesOf(profile.FirewallRules),
				Concurrency:   profile.Concurrency.ValueString(),
			}
		}
	}

	return clusterModel{
		Name:         model.Name.ValueString(),
		LoadBalancer: model.LoadBalancer.ValueString(),
		Values:       model.Values.ValueString(),
		Profiles:     profiles,
		Hosts:        hosts,
	}
}

// portsOf and rulesOf are the same boundary for the two shapes a host and a profile share.
func portsOf(attrs []portAttr) []portModel {
	if len(attrs) == 0 {
		return nil
	}
	ports := make([]portModel, 0, len(attrs))
	for _, attr := range attrs {
		ports = append(ports, portModel{
			Port:     attr.Port.ValueString(),
			Protocol: attr.Protocol.ValueString(),
		})
	}
	return ports
}

func rulesOf(attrs []firewallAttr) []firewallRuleModel {
	if len(attrs) == 0 {
		return nil
	}
	rules := make([]firewallRuleModel, 0, len(attrs))
	for _, attr := range attrs {
		rules = append(rules, firewallRuleModel{
			Name:        attr.Name.ValueString(),
			Action:      attr.Action.ValueString(),
			Direction:   attr.Direction.ValueString(),
			Source:      attr.Source.ValueString(),
			Destination: attr.Destination.ValueString(),
			Ingress:     attr.Ingress.ValueString(),
			Egress:      attr.Egress.ValueString(),
			Port:        attr.Port.ValueString(),
			Protocol:    attr.Protocol.ValueString(),
		})
	}
	return rules
}
