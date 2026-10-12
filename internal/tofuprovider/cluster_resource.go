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

func newClusterResource() resource.Resource {
	return &clusterResource{}
}

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

	ID            types.String `tfsdk:"id"`
	Distro        types.String `tfsdk:"distro"`
	EngineVersion types.String `tfsdk:"engine_version"`
	Kubeconfig    types.String `tfsdk:"kubeconfig"`

	Hosts map[string]factsHost `tfsdk:"hosts"`
	Nodes []factsNode          `tfsdk:"nodes"`
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
			"hosts": schema.MapNestedAttribute{
				MarkdownDescription: "The fleet, keyed by hostname or an identifier of your choosing. The key names " +
					"the host when `hostname` is not set, and it is what orders the run: controllers are acted on " +
					"first, and the controller whose key sorts first becomes the leader.",
				Optional: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: hostBlockAttributes(),
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
	r.converge(ctx, &model, &resp.Diagnostics, &resp.State)
}

// Update is the same call as Create. See the type comment.
func (r *clusterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var model clusterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.converge(ctx, &model, &resp.Diagnostics, &resp.State)
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

	model.Nodes = nodesOf(facts)
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

	cfg, err := translate(ctx, clusterModelOf(model))
	if err != nil {
		resp.Diagnostics.AddError("Invalid cluster configuration", err.Error())
		return
	}

	timeout, diag := durationOf(model.Timeout, "timeout")
	if diag != nil {
		resp.Diagnostics.Append(diag)
		return
	}

	tflog.Info(ctx, "resetting the cluster", map[string]any{"cluster": model.Name.ValueString()})
	if err := r.converger.Teardown(ctx, cfg, teardownOptions{
		DistroID:         r.resolveDistro(ctx, &model),
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
func (r *clusterResource) converge(ctx context.Context, model *clusterResourceModel, diags *diag.Diagnostics, state *tfsdk.State) {
	cfg, err := translate(ctx, clusterModelOf(*model))
	if err != nil {
		diags.AddError("Invalid cluster configuration", err.Error())
		return
	}

	timeout, diag := durationOf(model.Timeout, "timeout")
	if diag != nil {
		diags.Append(diag)
		return
	}

	tflog.Info(ctx, "converging the cluster", map[string]any{
		"cluster": model.Name.ValueString(),
		"package": model.Package.ValueString(),
		"hosts":   len(model.Hosts),
	})

	result, runErr := r.converger.Apply(ctx, cfg, applyOptions{
		Package:             model.Package.ValueString(),
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
	model.Nodes = nodesOf(result.Facts)
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
	// A map has no order, and the run does: controllers go first and the first of them leads. So
	// the keys are sorted, which makes the leader the controller whose key sorts first -- stable
	// across plans, and stated in the attribute's description.
	keys := make([]string, 0, len(model.Hosts))
	for k := range model.Hosts {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	hosts := make([]hostModel, 0, len(model.Hosts))
	for _, k := range keys {
		host := model.Hosts[k]
		hostname := host.Hostname.ValueString()
		if hostname == "" {
			hostname = k
		}
		hosts = append(hosts, hostModel{
			Address:        host.Address.ValueString(),
			User:           host.User.ValueString(),
			Port:           int(host.Port.ValueInt64()),
			KeyPath:        host.KeyPath.ValueString(),
			Role:           host.Role.ValueString(),
			Profile:        host.Profile.ValueString(),
			Hostname:       hostname,
			PrivateAddress: host.PrivateAddress.ValueString(),
		})
	}
	return clusterModel{
		Name:         model.Name.ValueString(),
		LoadBalancer: model.LoadBalancer.ValueString(),
		Hosts:        hosts,
	}
}
