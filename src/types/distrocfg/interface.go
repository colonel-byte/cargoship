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

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
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

// Distro interface for any distro object
type Distro interface {
	//keep-sorted start sticky_comments=yes
	// BinaryName returns the engine binary name
	BinaryName() string
	// BinaryPath returns the full path to the engine binary
	BinaryPath() string
	// ConfigPath returns the full path for the config directory used by the engine
	ConfigPath() string
	// ConfigureEngine does distro specific configuration on a host
	ConfigureEngine(context.Context, cluster.ZarfHost, cluster.ZarfRuntimeMeta, distro.ZarfDistro) error
	// DataDirPath returns the full path for the data directory used by the engine
	DataDirPath() string
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
	KubeconfigPath(cluster.ZarfHost, string) string
	// KubectlCmdf returns a string with that can be executed to interact with the kubernetes cluster
	KubectlCmdf(cluster.ZarfHost, string, string, ...any) string
	// RunningVersion returns the version of the distro being ran, if the engine is not running it throws an "ErrVersionNotDetected" error
	RunningVersion(cluster.ZarfHost) (string, error)
	// SetPath takes in a key value pair to change how the distro values are configured, if a key is not valid it will throw an "ErrPathKey" error
	SetPath(key string, value string) error
	// StopControllerService stops the controller service on the host
	StopControllerService(*cluster.ZarfHost) error
	// StopWorkerService stops the controller service on the host
	StopWorkerService(*cluster.ZarfHost) error
	//keep-sorted end
}
