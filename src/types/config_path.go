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

const (
	// ZarfCache path in config
	ZarfCache = "zarf_cache"
	// TmpDir path in config
	TmpDir = "tmp_dir"
	// LogLevel path in config
	LogLevel = "log_level"
	// LogFormat path in config
	LogFormat = "log_format"
	// NoColor path in config
	NoColor = "no_color"
	// LoggingLevelDefault path in config
	LoggingLevelDefault = "info"
	// DistroCreateOutput path in config
	DistroCreateOutput = "distro.create.output"
	// DistroOCIConcurrency path in config
	DistroOCIConcurrency = "distro.oci_concurrency"
	// DistroCreateRegistryOverride path in config
	DistroCreateRegistryOverride = "distro.create.registry_override"
	// DistroCreateSkipSbom path in config
	DistroCreateSkipSbom = "distro.create.skip_sbom"
	// DistroConcurrency path in config
	DistroConcurrency = "distro.concurrency"
	// DistroWorkerConcurrency path in config
	DistroWorkerConcurrency = "distro.worker_concurrency"
	// DistroFAPolicy path in config
	DistroFAPolicy = "distro.fapolicy"
	// DistroUpdateHost path in config
	DistroUpdateHost = "distro.host_update"
	// DistroUpdateFirewall path in config
	DistroUpdateFirewall = "distro.firewall_update"
	// DistroResetDistro path in config
	DistroResetDistro = "distro.reset.distro"
)
