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

package action

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/pkg/helmvalues"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// ManagerOptions are the inputs to NewManager.
type ManagerOptions struct {
	// Cluster is the parsed inventory. The CLI reads it from a file through
	// load.ClusterDefinition; a caller holding bytes uses cluster.Parse, and one assembling it in
	// memory -- a controller building hosts from somewhere other than a YAML file -- just fills
	// the struct in.
	Cluster cluster.Cluster
	// Layout is the extracted package, from distro.Load or distro.Build. Nil builds the manager
	// reset and kube-config build, which load no package because neither installs anything;
	// DistroID is then the only way to name the engine, and TempDirectory is left empty so there
	// is nothing for the caller to clean up.
	Layout *layout.DistroLayout
	// DistroID names the engine. It is read only when Layout is nil, because a layout carries the
	// authoritative answer in its own distro definition.
	DistroID string
	// ValueOverrides are merged after Cluster.Spec.Config.Values, in order, last wins. The CLI
	// passes what --values read, so that the command line beats the inventory.
	ValueOverrides []map[string]any
	// Concurrency caps how many hosts a phase acts on at once, and is used for uploads too. Zero
	// means unlimited; a negative value is clamped to zero rather than refused.
	Concurrency int
	// DryRun reaches phase.Manager.DryRun.
	DryRun bool
	// Timeout bounds the retry loops phases run through Manager.RetryTimeout. Zero leaves the
	// manager's own default in place.
	Timeout time.Duration
}

// NewManager resolves a package layout against a cluster inventory and builds the phase manager
// every action in this package takes.
//
// The ordering here is the point of the function. Values are merged and schema-checked, then
// ApplyValues renders and maps the engine configuration, then RenderFiles renders the contents of
// the files the package marked as templates, in place in the extracted package -- and all of it
// happens before any phase runs. Without ApplyValues and RenderFiles an install writes a flavor
// whose engine config still holds raw `{{ ... }}`, which reaches the host as invalid YAML;
// rke2-multi-cni-cilium's HelmChartConfig did exactly that, and the CNI never installed. The
// resolved values are handed to the manager as well, because the `when` gate on a templated file
// reads Manager.Values and a nil map has no path set, so every gated file is skipped for the whole
// run without it. Both renders are also given the cluster facts a template may read under
// .Cluster, which is how a manifest the package ships - a kube-vip static pod, say - reaches the
// control-plane address without the inventory having to state it twice.
//
// It lived in cmd/ as an unexported helper, with a second copy in the e2e harness, which is how
// the two of them came to resolve values differently. It is exported because the ordering is not
// discoverable from the parts: each step is already an exported method on the layout, and nothing
// about their signatures says they must be called in this order, once, before the manager is
// built. The Cluster API provider is the out-of-tree caller that needs it; see
// docs/agent/choice-capi-provider.md.
//
// The manager owns the extracted package at Manager.TempDirectory, which is the layout's own
// directory. Removing it stays with the caller, the way each command does it on the way out.
func NewManager(ctx context.Context, opts ManagerOptions) (*phase.Manager, error) {
	if opts.Concurrency < 0 {
		opts.Concurrency = 0
	}

	manager := &phase.Manager{
		Config:            &opts.Cluster,
		DistroID:          opts.DistroID,
		Concurrency:       opts.Concurrency,
		ConcurrentUploads: opts.Concurrency,
		DryRun:            opts.DryRun,
	}

	if opts.Layout != nil {
		logger.From(ctx).Debug("distro information", "temp", opts.Layout.DirPath(), "build", opts.Layout.Distro.Build.Timestamp)

		overrides := append([]map[string]any{opts.Cluster.Spec.Config.Values}, opts.ValueOverrides...)
		values, err := opts.Layout.Values(ctx, overrides...)
		if err != nil {
			return nil, err
		}
		// The cluster facts a package's templates may read, resolved here for the same
		// reason the values are: both renders have to see the same inputs, and the
		// inventory is in scope here and in no phase afterwards.
		facts := layout.ClusterFacts{LoadBalancer: opts.Cluster.Spec.Config.LoadBalancer}
		if err := opts.Layout.ApplyValues(values, facts); err != nil {
			return nil, err
		}
		if err := opts.Layout.RenderFiles(ctx, values, facts); err != nil {
			return nil, fmt.Errorf("failed to render the templated files of the package: %w", err)
		}
		// A chart's `when` gate, resolved once. The engine implementation that writes the
		// HelmChart manifest is handed the package and not the cluster's values, so the gate
		// has to be answered here; the chart's tarball is gated separately, by the file entry
		// the chart expanded into, through the upload phase's own reading of the same path.
		//
		// Only an explicit false gates a chart off. A path that cannot be read, is not set,
		// or does not hold a boolean is an error rather than a skip: a skipped chart has its
		// manifest pruned from the engine's manifest directory, which the helm controller
		// answers by uninstalling the release -- so a renamed values key or an override that
		// replaced the gate's parent map would take a running workload down, with one warning
		// line as the only trace.
		var gateErr error
		opts.Layout.Distro.Spec.Config.ResolveChartDelivery(func(path string) bool {
			deliver, err := chartGate(values, path)
			if err != nil {
				gateErr = errors.Join(gateErr, err)
				return false
			}
			return deliver
		})
		if gateErr != nil {
			return nil, fmt.Errorf("failed to resolve which charts this cluster asked for: %w", gateErr)
		}
		// Read after the gate, so a chart the cluster asked for is the only one whose bytes
		// are loaded.
		if err := opts.Layout.LoadCharts(ctx); err != nil {
			return nil, err
		}

		manager.Distro = &opts.Layout.Distro
		manager.Values = values
		manager.DistroID = opts.Layout.Distro.Spec.Type
		manager.TempDirectory = opts.Layout.DirPath()
	}

	if opts.Timeout > 0 {
		manager.SetTimeout(opts.Timeout)
	}

	return manager, nil
}

// chartGate answers one chart's when path out of a cluster's resolved values.
//
// Only an explicit false gates a chart off. Everything else is an error, because a chart left
// out of the delivered set has its manifest pruned from the engine's manifest directory and the
// helm controller answers that by uninstalling the release -- so a renamed values key, or an
// override that replaced the gate's parent map, would take a running workload down.
func chartGate(values map[string]any, path string) (bool, error) {
	v, ok, err := helmvalues.GetValuePath(values, path)
	if err != nil {
		return false, fmt.Errorf("reading the when path %s: %w", path, err)
	}
	if !ok {
		return false, fmt.Errorf("the when path %s is not set in the package's values", path)
	}
	enabled, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("the when path %s holds %T rather than a boolean", path, v)
	}
	return enabled, nil
}
