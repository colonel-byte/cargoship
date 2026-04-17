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

	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/distro"
	"github.com/k0sproject/rig/os"
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
	GetClusterCIDR(distro.ZarfDistro) []string
	GetControllerService() string
	GetWorkerService() string
	JoinTokenPath() string
	JoinTokenPathAgent() string
	KubeconfigPath(os.Host, string) string
	KubectlCmdf(os.Host, string, string, ...any) string
	SetPath(string, string) error
	//keep-sorted end
}
