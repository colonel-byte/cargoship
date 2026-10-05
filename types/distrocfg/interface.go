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

// Package distrocfg defines the standard interface that all distro config settings
package distrocfg

import (
	"context"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
)

const (
	// Binary id string
	Binary = "Binary"
	// BinaryDir id string
	BinaryDir = "BinDir"
	// Config id string
	Config = "Config"
	// Token id string
	Token = "Token"
	// Data id string
	Data = "DataDir"
	// WorkerService id string
	WorkerService = "Worker"
	// ControllerService id string
	ControllerService = "Control"
)

// DesiredFile is one engine config file a distro wants on a host: its content, and the mode it
// is written with. The mode travels with the content because it varies per file -- a file the
// engine reads as a group member is not written like one holding credentials.
type DesiredFile struct {
	// Content is the full desired content of the file.
	Content []byte
	// Mode is the file mode as chmod spells it, e.g. "0600".
	Mode string
	// NoRestart indicates changes to this file do not require draining or restarting the engine.
	NoRestart bool
}

// Distro interface for any distro object
type Distro interface {
	// AdminCredentials returns the cluster CA certificate and the admin client key pair
	// for a given controller host and data directory
	AdminCredentials(*cluster.ZarfHost, string) (AdminCredentials, error)
	// BinaryName returns the engine binary name
	BinaryName() string
	// BinaryPath returns the full path to the engine binary
	BinaryPath() string
	// CleanupPaths returns every path on a host the engine owns outright, for an uninstall
	// to remove recursively. Paths that are unset or too broad to safely remove are left out.
	CleanupPaths() []string
	// ConfigPath returns the full path for the config directory used by the engine
	ConfigPath() string
	// ConfigureEngine does distro specific configuration on a host
	ConfigureEngine(context.Context, *cluster.ZarfHost, cluster.ZarfRuntimeMeta, distro.ZarfDistro) error
	// DataDirPath returns the full path for the data directory used by the engine
	DataDirPath() string
	// DesiredFiles returns the full set of engine config files (path -> desired file) this
	// distro would write for the given host/run/dis state -- e.g. registries.yaml, audit.yaml,
	// pss.yaml -- used both to pre-seed a fresh host and, by the engine-config-sync phases, to
	// detect drift on an already-running host.
	DesiredFiles(*cluster.ZarfHost, cluster.ZarfRuntimeMeta, distro.ZarfDistro) (map[string]DesiredFile, error)
	// ManagedDirs returns the directories on a host cargoship prunes, so that a file in one of
	// them that DesiredFiles no longer names can be removed rather than left behind. A
	// directory cargoship shares with the engine names the files that are its own. A distro
	// that keeps no such directory returns nil.
	ManagedDirs() []ManagedDir
	// PackageStagingDir returns the directory staged .rpm/.deb package files are uploaded to,
	// so the uninstall phase knows where to recover installed package names from. A distro that
	// installs from a single binary rather than staged package files shares this with
	// DataDirPath, since nothing else uses the value.
	PackageStagingDir() string
	// DistroCmdf returns a string that can be used to execute commands on the core engine binary
	DistroCmdf(string, ...any) string
	// GetClusterCIDR returns a string array with the all the known cluster cidr blocks
	GetClusterCIDR(distro.ZarfDistro) []string
	// GetControllerService returns the name of the controller service
	GetControllerService() string
	// GetWorkerService returns the name of the worker service
	GetWorkerService() string
	// JoinTokenPath returns the path of the token to join the cluster
	JoinTokenPath() string
	// JoinTokenPathAgent returns the path of the token to join the cluster.
	// Distro's like RKE2 and K3S allow for agent tokens, so this allows for some level of access control if a node is allowed to be a controller or an agent.
	JoinTokenPathAgent() string
	// KubeconfigPath returns the path to the admin config for a given
	KubeconfigPath(*cluster.ZarfHost, string) string
	// KubectlCmdf returns a string with that can be executed to interact with the kubernetes cluster
	KubectlCmdf(*cluster.ZarfHost, string, string, ...any) string
	// RunningVersion returns the version of the distro being ran, if the engine is not running it throws an "ErrVersionNotDetected" error
	RunningVersion(*cluster.ZarfHost) (string, error)
	// SetPath takes in a key value pair to change how the distro values are configured, if a key is not valid it will throw an "ErrPathKey" error
	SetPath(key string, value string) error
	// StopControllerService stops the controller service on the host
	StopControllerService(*cluster.ZarfHost) error
	// StopWorkerService stops the controller service on the host
	StopWorkerService(*cluster.ZarfHost) error
}

// ImageImporter is implemented by a distro whose engine does not import uploaded image tarballs
// on its own, so cargoship must trigger the import itself. rke2 and k3s's own agents import
// automatically outside cargoship's control and do not implement this; a distro without an
// agent to do that -- plain kubeadm's upstream containerd, say -- does.
type ImageImporter interface {
	// ImportImages imports every image tarball staged under path into the engine's image store.
	ImportImages(host *cluster.ZarfHost, path string) error
}

// Bootstrapper is implemented by a distro whose cluster formation is a one-shot command rather
// than a service to start -- kubeadm init/join, unlike rke2/k3s's single config.yaml + systemd
// service. InitializeControllers/InitializeWorkers call Bootstrap instead of starting the
// service, and ask IsBootstrapped instead of checking service state, when a distro implements
// this, the same optional-interface pattern ImageImporter uses.
type Bootstrapper interface {
	// IsBootstrapped reports whether host has already formed or joined the cluster, so a re-run
	// is a no-op instead of re-running the bootstrap command.
	IsBootstrapped(host *cluster.ZarfHost) bool
	// Bootstrap forms the cluster (the leader) or joins it (every other host).
	Bootstrap(ctx context.Context, host *cluster.ZarfHost, run cluster.ZarfRuntimeMeta, dis distro.ZarfDistro) error
}

// PreStartUpgrader is implemented by a distro whose upgrade needs a step between the new package
// install and the service restart -- kubeadm upgrade, unlike rke2/k3s where a plain restart on the
// new binary is enough. UpgradeHosts calls it, when implemented, right after installDistro and
// before startService. Same optional-interface pattern as Bootstrapper.
type PreStartUpgrader interface {
	// PreStartUpgrade runs after the new packages are staged but before the service restarts.
	PreStartUpgrade(ctx context.Context, host *cluster.ZarfHost, dis distro.ZarfDistro) error
}

// PreUninstallResetter is implemented by a distro that must tear down its own cluster membership
// before its packages are removed -- kubeadm reset, unlike rke2/k3s where uninstalling the package
// is the whole story. UninstallEngine calls it, when implemented, before resolving package names
// and removing them. Same optional-interface pattern as PreStartUpgrader.
type PreUninstallResetter interface {
	// PreUninstallReset runs before packages are resolved and removed.
	PreUninstallReset(ctx context.Context, host *cluster.ZarfHost) error
}

// ManifestApplier is implemented by a distro whose package can declare raw manifests -- typically
// a CNI -- that cargoship applies with kubectl once the leader is reachable, instead of rendering
// them into a HelmChartConfig an embedded controller reconciles (rancher_common.go's
// helmChartConfigs). Same optional-interface pattern as ImageImporter/Bootstrapper.
type ManifestApplier interface {
	// ManifestPaths returns the on-host paths to kubectl apply, in order. Empty when the package
	// declares none.
	ManifestPaths(dis distro.ZarfDistro) []string
}
