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

package types

type DistroConfig struct {
	//keep-sorted start
	CachePath     string        `json:"zarf_cache,omitempty"`
	DistroOpts    DistroOptions `json:"distro,omitempty"`
	LogFormat     string        `json:"log_format,omitempty" jsonschema:"enum=console,enum=json,enum=dev,default=console"`
	LogLevel      string        `json:"log_level,omitempty" jsonschema:"enum=warn,enum=info,enum=debug,enum=trace,default=info"`
	TempDirectory string        `json:"tmp_dir,omitempty" jsonschema:"default=/tmp"`
	Timeout       string        `json:"timeout,omitempty" jsonschema:"default=20m"`
	//keep-sorted end
}

type DistroOptions struct {
	OCIConcurrency int                 `json:"oci_concurrency,omitempty"`
	CreateOpts     DistroCreateOptions `json:"create,omitempty"`
	DeployOpts     DistroDeployOptions `json:"deploy,omitempty"`
	InstallOpts    InstallOptions      `json:"install,omitempty"`
}

type DistroCreateOptions struct {
	//keep-sorted start
	CachePath string `json:"cache_path,omitempty"`
	Name      string `json:"name,omitempty"`
	Output    string `json:"output,omitempty"`
	SkipSBOM  bool   `json:"skip_sbom,omitempty"`
	Version   string `json:"version,omitempty"`
	//keep-sorted end
	SourceDirectory string `json:"-"`
}

type DistroDeployOptions struct {
	Retries int `json:"retries,omitempty"`
}

type InstallOptions struct {
	//keep-sorted start
	Concurrency       int  `json:"concurrency,omitempty" jsonschema:"minimum=0"`
	FirewallUpdate    bool `json:"firewall_update,omitempty" jsonschema:"default=true"`
	HostUpdate        bool `json:"host_update,omitempty" jsonschema:"default=true"`
	WorkerConcurrency int  `json:"worker_concurrency,omitempty" jsonschema:"minimum=0"`
	//keep-sorted end
}
