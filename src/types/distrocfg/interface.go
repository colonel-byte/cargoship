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

package distrocfg

import (
	"context"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
)

const (
	Binary            = "Binary"
	BinaryDir         = "BinDir"
	Config            = "Config"
	Token             = "Token"
	Data              = "DataDir"
	WorkerService     = "Worker"
	ControllerService = "Control"
)

type Distro interface {
	//keep-sorted start
	BinaryName() string
	BinaryPath() string
	ConfigPath() string
	ConfigureEngine(context.Context, cluster.ZarfHost, cluster.ZarfRuntimeMeta, distro.ZarfDistro) error
	DataDirPath() string
	DistroCmdf(string, ...any) string
	GetClusterCIDR(distro.ZarfDistro) []string
	GetControllerService() string
	GetWorkerService() string
	JoinTokenPath() string
	JoinTokenPathAgent() string
	KubeconfigPath(cluster.ZarfHost, string) string
	KubectlCmdf(cluster.ZarfHost, string, string, ...any) string
	RunningVersion(cluster.ZarfHost) (string, error)
	SetPath(string, string) error
	StopControllerService(*cluster.ZarfHost) error
	StopWorkerService(*cluster.ZarfHost) error
	//keep-sorted end
}
