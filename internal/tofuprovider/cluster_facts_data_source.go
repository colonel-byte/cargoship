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
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// clusterFactsDataSource reads what cargoship can learn about a fleet without changing it.
//
// It is the first thing the provider does, and it is read-only on purpose. Every part of the
// plumbing a resource will need -- the schema, the translation into a cluster document, the SSH
// connections, the logging, cancellation -- is exercised here, where the worst outcome of a bug is
// a failed plan rather than a changed host. It is also useful on its own: the engine version each
// host reports is what a configuration elsewhere can route on.
type clusterFactsDataSource struct {
	converger converger
}

func newClusterFactsDataSource() datasource.DataSource {
	return &clusterFactsDataSource{}
}

// factsModel is the data source's state.
type factsModel struct {
	Name         types.String `tfsdk:"name"`
	LoadBalancer types.String `tfsdk:"load_balancer"`
	Distro       types.String `tfsdk:"distro"`
	Hosts        []factsHost  `tfsdk:"host"`
	Nodes        []factsNode  `tfsdk:"nodes"`
}

// factsHost is one host block of the data source.
type factsHost struct {
	Address        types.String `tfsdk:"address"`
	User           types.String `tfsdk:"user"`
	Port           types.Int64  `tfsdk:"port"`
	KeyPath        types.String `tfsdk:"key_path"`
	Role           types.String `tfsdk:"role"`
	Profile        types.String `tfsdk:"profile"`
	Hostname       types.String `tfsdk:"hostname"`
	PrivateAddress types.String `tfsdk:"private_address"`
}

// factsNode is what was read back about one host.
type factsNode struct {
	Address        types.String `tfsdk:"address"`
	Hostname       types.String `tfsdk:"hostname"`
	OS             types.String `tfsdk:"os"`
	OSVersion      types.String `tfsdk:"os_version"`
	Arch           types.String `tfsdk:"arch"`
	PrivateAddress types.String `tfsdk:"private_address"`
	Role           types.String `tfsdk:"role"`
	EngineVersion  types.String `tfsdk:"engine_version"`
}

func (d *clusterFactsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster_facts"
}

func (d *clusterFactsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads what cargoship can learn about a fleet without changing it: each host's operating " +
			"system, architecture, hostname, private address, and the engine version it is running. It runs the five " +
			"read-only phases of an apply (`Connect`, `DetectOS`, `GatherFacts`, `GatherFactsDistro`, `Disconnect`), " +
			"takes no cluster lock, and writes nothing to any host.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "The cluster's name. It becomes `metadata.name` in the cluster document cargoship reads.",
				Required:            true,
			},
			"load_balancer": schema.StringAttribute{
				MarkdownDescription: "The address clients use to reach the control plane. Nothing is contacted through " +
					"it here -- the facts are read over SSH -- but the cluster document requires it.",
				Required: true,
			},
			"distro": schema.StringAttribute{
				MarkdownDescription: "The engine to read: `k3s` or `rke2`. It is what tells cargoship which version " +
					"string to look for on each host.",
				Required: true,
			},
			"nodes": schema.ListNestedAttribute{
				MarkdownDescription: "What was read back, one entry per host block, in the same order.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
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
				},
			},
		},
		Blocks: map[string]schema.Block{
			"host": schema.ListNestedBlock{
				MarkdownDescription: "A host to read. Controllers are read first whatever order the blocks are in, " +
					"because that is the order an apply uses.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
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
							MarkdownDescription: "Path to the SSH private key on the machine running OpenTofu. " +
								"There is no attribute for key material, and there will not be: a key in the " +
								"configuration is a key in the state file.",
							Optional: true,
						},
						"profile": schema.StringAttribute{
							MarkdownDescription: "The profile this host selects from the cluster's profile map.",
							Optional:            true,
						},
						"hostname": schema.StringAttribute{
							MarkdownDescription: "The name the host is known by before it has been visited.",
							Optional:            true,
						},
						"private_address": schema.StringAttribute{
							MarkdownDescription: "Overrides the private address the facts phase would discover.",
							Optional:            true,
						},
					},
				},
			},
		},
	}
}

// Configure takes the converger the provider built.
func (d *clusterFactsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		// The framework calls Configure before the provider has been configured during
		// validation. Nothing to do, and nothing wrong.
		return
	}
	c, ok := req.ProviderData.(converger)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("the data source was configured with a %T rather than something that can reach cargoship; this is a bug in the provider", req.ProviderData),
		)
		return
	}
	d.converger = c
}

// Read opens the connections, gathers the facts, and writes them to state.
func (d *clusterFactsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model factsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg, err := translate(ctx, modelOf(model))
	if err != nil {
		resp.Diagnostics.AddError("Invalid cluster configuration", err.Error())
		return
	}

	tflog.Debug(ctx, "gathering facts", map[string]any{
		"cluster": model.Name.ValueString(),
		"distro":  model.Distro.ValueString(),
		"hosts":   len(model.Hosts),
	})

	facts, err := d.converger.Refresh(ctx, cfg, model.Distro.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read the cluster's facts",
			fmt.Sprintf("cargoship could not gather facts for %s: %s", model.Name.ValueString(), err),
		)
		return
	}

	model.Nodes = nodesOf(facts)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

// modelOf is the translation boundary: the framework's types on one side, plain Go on the other,
// so translate and everything under it never sees a types.String.
func modelOf(model factsModel) clusterModel {
	hosts := make([]hostModel, 0, len(model.Hosts))
	for _, host := range model.Hosts {
		hosts = append(hosts, hostModel{
			Address:        host.Address.ValueString(),
			User:           host.User.ValueString(),
			Port:           int(host.Port.ValueInt64()),
			KeyPath:        host.KeyPath.ValueString(),
			Role:           host.Role.ValueString(),
			Profile:        host.Profile.ValueString(),
			Hostname:       host.Hostname.ValueString(),
			PrivateAddress: host.PrivateAddress.ValueString(),
		})
	}
	return clusterModel{
		Name:         model.Name.ValueString(),
		LoadBalancer: model.LoadBalancer.ValueString(),
		Hosts:        hosts,
	}
}

// nodesOf renders gathered facts as state.
func nodesOf(facts []hostFacts) []factsNode {
	nodes := make([]factsNode, 0, len(facts))
	for _, fact := range facts {
		nodes = append(nodes, factsNode{
			Address:        types.StringValue(fact.Address),
			Hostname:       types.StringValue(fact.Hostname),
			OS:             types.StringValue(fact.OS),
			OSVersion:      types.StringValue(fact.OSVersion),
			Arch:           types.StringValue(fact.Arch),
			PrivateAddress: types.StringValue(fact.PrivateAddress),
			Role:           types.StringValue(fact.Role),
			EngineVersion:  types.StringValue(fact.EngineVersion),
		})
	}
	return nodes
}
