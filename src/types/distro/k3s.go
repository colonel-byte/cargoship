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

package distro

import (
	"maps"

	"github.com/colonel-byte/zarf-distro/src/types/distro/registry"
)

const (
	DISTRO_ID_K3S = "k3s"
)

type K3S struct {
	Common
}

var _ Distro = (*K3S)(nil)

func init() {
	registry.RegisterDistroModule(
		func(dis string) bool {
			return dis == DISTRO_ID_K3S
		},
		func() any {
			return &K3S{}
		},
	)
}

func (r *K3S) initPaths() {
	r.pathOnce.Do(func() {
		r.ID = DISTRO_ID_RKE2
		r.paths = map[string]string{
			BinaryDir: "/usr/local/bin",
			Binary:    "k3s",
			Config:    "/etc/rancher/k3s/config.yaml",
			Token:     "/etc/rancher/k3s/token",
			Data:      "/var/lib/rancher/k3s",
		}
		r.services = map[string]string{
			ControllerService: "k3s-server",
			WorkerService:     "k3s-worker",
		}
	})
}

func (r *K3S) path(key string) string {
	r.initPaths()
	r.pathMu.RLock()
	defer r.pathMu.RUnlock()
	return r.paths[key]
}

func (r *K3S) service(key string) string {
	r.initPaths()
	r.pathMu.RLock()
	defer r.pathMu.RUnlock()
	return r.services[key]
}

func (r *K3S) BinaryPath() string {
	return r.path(BinaryDir) + "/" + r.path(Binary)
}

func (r *K3S) BinaryName() string {
	return r.path(Binary)
}

func (r *K3S) ConfigPath() string {
	return r.path(Config)
}

func (r *K3S) JoinTokenPath() string {
	return r.path(Token)
}

func (r *K3S) DataDirDefaultPath() string {
	return r.path(Data)
}

func (r *K3S) GetServices() map[string]string {
	r.initPaths()
	return maps.Clone(r.services)
}

func (r *K3S) GetWorkerService() string {
	return r.service(WorkerService)
}

func (r *K3S) GetControllerService() string {
	return r.service(ControllerService)
}

func (r *K3S) GetPaths() map[string]string {
	return maps.Clone(r.paths)
}

func (r *K3S) SetPath(key string, value string) {
	r.initPaths()
	r.pathMu.Lock()
	defer r.pathMu.Unlock()
	r.paths[key] = value
}
