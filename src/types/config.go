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
	DistroOpts    DistroOptions  `json:"distro,omitempty"`
	InstallOpts   InstallOptions `json:"install,omitempty"`
	LogLevel      string         `json:"log_level,omitempty" jsonschema:"enum=warn,enum=info,enum=debug,enum=trace,default=info"`
	LogFormat     string         `json:"log_format,omitempty" jsonschema:"enum=console,enum=json,enum=dev,default=console"`
	CachePath     string         `json:"zarf_cache,omitempty"`
	TempDirectory string         `json:"tmp_dir,omitempty" jsonschema:"default=/tmp"`
}

type DistroOptions struct {
	OCIConcurrency int                 `json:"oci_concurrency,omitempty"`
	CreateOpts     DistroCreateOptions `json:"create,omitempty"`
	DeployOpts     DistroDeployOptions `json:"deploy,omitempty"`
}

type DistroCreateOptions struct {
	SourceDirectory string `json:"-"`
	Output          string `json:"output,omitempty"`
	Version         string `json:"version,omitempty"`
	Name            string `json:"name,omitempty"`
	CachePath       string `json:"cache_path,omitempty"`
	SkipSBOM        bool   `json:"skip_sbom,omitempty"`
}

type DistroDeployOptions struct {
	Retries int `json:"retries,omitempty"`
}

type InstallOptions struct {
	HostUpdate        bool `json:"host_update,omitempty" jsonschema:"default=true"`
	Concurrency       int  `json:"concurrency,omitempty" jsonschema:"minimum=0"`
	WorkerConcurrency int  `json:"worker_concurrency,omitempty" jsonschema:"minimum=0"`
}
