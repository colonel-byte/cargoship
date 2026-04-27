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

// Package types is a little bit of a hacky way to generate the cargo-ship-config jsonschema
package types

// DistroConfig holds the values for the `.`, or root, section of the config file
type DistroConfig struct {
	//keep-sorted start
	// CachePath is the folder where oras artifacts are stored
	CachePath string `json:"zarf_cache,omitempty"`
	// DistroOpts are various options used by the command
	DistroOpts DistroOptions `json:"distro,omitempty"`
	// LogFormat how the logs are displayed well running
	LogFormat string `json:"log_format,omitempty" jsonschema:"enum=console,enum=json,enum=dev,default=console"`
	// LogLevel the level of logs that will be displayed
	LogLevel string `json:"log_level,omitempty" jsonschema:"enum=warn,enum=info,enum=debug,enum=trace,default=info"`
	// TempDirectory the directory where we store stuff before deleting them
	TempDirectory string `json:"tmp_dir,omitempty" jsonschema:"default=/tmp"`
	// Timeout the longest we will run long ran tasks before failing
	Timeout string `json:"timeout,omitempty" jsonschema:"default=20m"`
	//keep-sorted end
}

// DistroOptions holds the values for the `.distro` section of the config file
type DistroOptions struct {
	// CreateOpts are options used by the create subcommand
	CreateOpts DistroCreateOptions `json:"create,omitempty"`
	// DeployOpts are options used by the deploy subcommand
	DeployOpts DistroDeployOptions `json:"deploy,omitempty"`
	// ApplyOpts are options used by the apply subcommand
	ApplyOpts ApplyOptions `json:"apply,omitempty"`
	// ResetOptions are options used by the reset subcommand
	ResetOpts ResetOptions `json:"reset,omitempty"`
	// OCIConcurrency is how many concurrent oci artifacts that will be pushed at a time
	OCIConcurrency int `json:"oci_concurrency,omitempty"`
}

// DistroCreateOptions holds the values for the `.distro.create` section of the config file
type DistroCreateOptions struct {
	//keep-sorted start
	// Output the folder that we will create the distro tar balls in
	Output string `json:"output,omitempty"`
	// SkipSBOM whether we will scan the images or files
	SkipSBOM bool `json:"skip_sbom,omitempty"`
	//keep-sorted end
}

// DistroDeployOptions holds the values for the `.distro.deploy` section of the config file
type DistroDeployOptions struct {
	// Retries how many times we will try to push a package
	Retries int `json:"retries,omitempty"`
}

// ApplyOptions holds the values for the `.distro.apply` section of the config file
type ApplyOptions struct {
	//keep-sorted start
	// Concurrency how many nodes we will try to interact with at a time, 0 means that all nodes will be done at once
	Concurrency int `json:"concurrency,omitempty" jsonschema:"minimum=0"`
	// FirewallUpdate whether we will update the host firewall
	FirewallUpdate bool `json:"firewall_update,omitempty" jsonschema:"default=true"`
	// HostUpdate whether we will update the etc host file
	HostUpdate bool `json:"host_update,omitempty" jsonschema:"default=true"`
	// WorkerConcurrency number of worker nodes that will be upgraded at once
	WorkerConcurrency int `json:"worker_concurrency,omitempty" jsonschema:"minimum=0"`
	//keep-sorted end
}

// ResetOptions holds the values for the `.distro.reset` section of the config file
type ResetOptions struct {
	// Concurrency how many nodes we will try to interact with at a time, 0 means that all nodes will be done at once
	Concurrency int `json:"concurrency,omitempty" jsonschema:"minimum=0"`
	// FirewallUpdate whether we will update the host firewall
	FirewallUpdate bool `json:"firewall_update,omitempty" jsonschema:"default=true"`
	// HostUpdate whether we will update the etc host file
	HostUpdate bool `json:"host_update,omitempty" jsonschema:"default=true"`
	// WorkerConcurrency number of worker nodes that will be upgraded at once
	WorkerConcurrency int `json:"worker_concurrency,omitempty" jsonschema:"minimum=0"`
	// Distro that is used to determine how to remove files
	Distro string `json:"distro,omitempty" jsonschema:"enum=rke2,enum=k3s"`
}
