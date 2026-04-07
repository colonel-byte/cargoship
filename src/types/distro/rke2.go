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
	DISTRO_ID_RKE2 = "rke2"
)

type RKE2 struct {
	Common
}

var _ Distro = (*RKE2)(nil)

func init() {
	registry.RegisterDistroModule(
		func(dis string) bool {
			return dis == DISTRO_ID_RKE2
		},
		func() any {
			return &RKE2{}
		},
	)
}

func (r *RKE2) initPaths() {
	r.pathOnce.Do(func() {
		r.ID = DISTRO_ID_RKE2
		r.paths = map[string]string{
			BinaryDir: "/usr/local/bin",
			Binary:    "rke2",
			Config:    "/etc/rancher/rke2/config.yaml",
			Token:     "/etc/rancher/rke2/token",
			Data:      "/var/lib/rancher/rke2",
		}
		r.services = map[string]string{
			ControllerService: "rke2-server",
			WorkerService:     "rke2-worker",
		}
	})
}

func (r *RKE2) path(key string) string {
	r.initPaths()
	r.pathMu.RLock()
	defer r.pathMu.RUnlock()
	return r.paths[key]
}

func (r *RKE2) service(key string) string {
	r.initPaths()
	r.pathMu.RLock()
	defer r.pathMu.RUnlock()
	return r.services[key]
}

func (r *RKE2) BinaryPath() string {
	return r.path(BinaryDir) + "/" + r.path(Binary)
}

func (r *RKE2) BinaryName() string {
	return r.path(Binary)
}

func (r *RKE2) ConfigPath() string {
	return r.path(Config)
}

func (r *RKE2) JoinTokenPath() string {
	return r.path(Token)
}

func (r *RKE2) DataDirDefaultPath() string {
	return r.path(Data)
}

func (r *RKE2) GetServices() map[string]string {
	r.initPaths()
	return maps.Clone(r.services)
}

func (r *RKE2) GetWorkerService() string {
	return r.service(WorkerService)
}

func (r *RKE2) GetControllerService() string {
	return r.service(ControllerService)
}

func (r *RKE2) GetPaths() map[string]string {
	r.initPaths()
	return maps.Clone(r.paths)
}

func (r *RKE2) SetPath(key string, value string) {
	r.initPaths()
	r.pathMu.Lock()
	defer r.pathMu.Unlock()
	r.paths[key] = value
}
