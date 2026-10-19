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
	"io"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Version is the provider version reported to OpenTofu. It is overwritten at build time with the
// release the binary was cut from; see magefiles/pkg/release.
var Version = "dev"

// cargoshipProvider is the provider itself.
//
// There is deliberately very little in its schema. Everything that describes a cluster belongs to
// a resource or a data source, and everything that is a credential is read from the environment
// rather than taken as an attribute -- see rule 2 of docs/agent/choice-tofu-secrets.md. What is
// left is the handful of settings that apply to every call: how wide to act on the fleet, and
// which cargoship to be.
type cargoshipProvider struct {
	// converger is what the resources and data sources call into. It is a field so a test can
	// install a fake one; New leaves it nil and Configure fills it in.
	converger converger
	// out is where phase progress writing goes, io.Discard when nil.
	out io.Writer
}

// providerModel is the provider block.
type providerModel struct {
	Concurrency    types.Int64  `tfsdk:"concurrency"`
	ConnectTimeout types.String `tfsdk:"connect_timeout"`
}

// New returns the provider OpenTofu serves.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		Version = version
		return &cargoshipProvider{}
	}
}

// NewWithConverger returns a provider that calls into c rather than into cargoship. It exists for
// the tests: the acceptance tests drive the real thing, and the unit tests drive this.
func NewWithConverger(c converger) func() provider.Provider {
	return func() provider.Provider {
		return &cargoshipProvider{converger: c}
	}
}

// Metadata names the provider, which is what makes the resources `cargoship_*`.
func (p *cargoshipProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "cargoship"
	resp.Version = Version
}

// Schema declares the provider block.
func (p *cargoshipProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Drives cargoship: the configuration describes the fleet, cargoship owns the phase ordering, " +
			"and an apply converges the cluster. Credentials are read from the environment rather than taken here -- " +
			"`CARGOSHIP_VAULT_PASSWORD` or `ANSIBLE_VAULT_PASSWORD` for an Ansible Vault password, " +
			"`CARGOSHIP_AGE_IDENTITY_FILE` for an age identity -- so that no key material reaches the state file.",
		Attributes: map[string]schema.Attribute{
			"concurrency": schema.Int64Attribute{
				MarkdownDescription: "How many hosts a phase acts on at once. Zero, the default, is unlimited. " +
					"It is the provider-wide equivalent of cargoship's `--concurrency`.",
				Optional: true,
			},
			"connect_timeout": schema.StringAttribute{
				MarkdownDescription: "How long to wait for the fleet to answer, as a Go duration (`30s`, `5m`). " +
					"Defaults to `1m`. cargoship's connect phase retries for ten minutes, which is right for an " +
					"apply somebody is watching and wrong for a plan: without this, a typo in an address would " +
					"make a plan sit silent for ten minutes. Raise it for a fleet that is genuinely slow to answer.",
				Optional: true,
			},
		},
	}
}

// Configure reads the provider block and builds what the resources call into.
func (p *cargoshipProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var model providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A provider built by NewWithConverger already holds one, and must keep it: that is the whole
	// point of the seam.
	if p.converger == nil {
		concurrency := 0
		if !model.Concurrency.IsNull() && !model.Concurrency.IsUnknown() {
			concurrency = int(model.Concurrency.ValueInt64())
		}
		if concurrency < 0 {
			concurrency = 0
		}

		timeout := DefaultConnectTimeout
		if value := model.ConnectTimeout.ValueString(); value != "" {
			parsed, err := time.ParseDuration(value)
			if err != nil {
				resp.Diagnostics.AddAttributeError(
					path.Root("connect_timeout"),
					"Invalid duration",
					fmt.Sprintf("connect_timeout %q is not a Go duration such as 30s or 5m: %s", value, err),
				)
				return
			}
			timeout = parsed
		}

		p.converger = cargoshipConverger{
			concurrency:    concurrency,
			connectTimeout: timeout,
			out:            p.out,
		}
	}

	// Both halves get the same value. A data source reads during plan and refresh, a resource
	// during apply, and neither should behave differently from the other.
	resp.DataSourceData = p.converger
	resp.ResourceData = p.converger
}

// Resources are the resources the provider serves.
func (p *cargoshipProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newClusterResource,
	}
}

// DataSources are the data sources the provider serves.
func (p *cargoshipProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		newClusterFactsDataSource,
	}
}
