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

package cluster

import (
	"errors"
	"fmt"
	gos "os"
	"slices"
	"time"

	configurer "github.com/colonel-byte/zarf-distro/src/types/os"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/os/registry"
)

const (
	ROLE_CONTROLLER        = "controller"
	ROLE_CONTROLLER_WORKER = "controller+worker"
	ROLE_SINGLE            = "single"
	ROLE_WORKER            = "worker"
	ROLE_ERROR             = "error"
)

// ErrCommandFailed is returned when a command fails
var ErrCommandFailed = errors.New("command failed")

type ZarfHost struct {
	rig.Connection `json:",inline"`
	//keep-sorted start
	Environment      map[string]string  `json:"environment,omitempty"`
	Files            []ZarfClusterFiles `json:"files,omitempty"`
	Hostname         string             `json:"hostname,omitempty"`
	NodeLabels       map[string]string  `json:"labels,omitempty"`
	NodeTaints       []string           `json:"taints,omitempty"`
	PrivateAddress   string             `json:"privateAddress,omitempty"`
	PrivateInterface string             `json:"privateInterface,omitempty"`
	Profile          string             `json:"profile,omitempty" `
	Role             string             `json:"role" jsonschema:"enum=controller,enum=controller+worker,enum=single,enum=worker"`
	//keep-sorted end
	Configurer configurer.Configurer `json:"-"`
	Metadata   ZarfHostMetadata      `json:"-"`
}

type ZarfHostMetadata struct {
	//keep-sorted start
	Arch           string
	BinaryTempFile []string
	DistroVersion  string
	EngineUploaded bool
	ExistingConfig string
	Hostname       string
	Installed      bool
	IsLeader       bool
	MachineID      string
	NeedsUpgrade   bool
	NewConfig      string
	Ready          bool
	//keep-sorted end
}

func (h *ZarfHost) requireConfigurer() (configurer.Configurer, error) {
	if h.Configurer == nil {
		return nil, fmt.Errorf("%s: host configurer is not resolved", h)
	}
	return h.Configurer, nil
}

// Dir returns the configurer-specific directory name for the given path.
func (h *ZarfHost) Dir(path string) (string, error) {
	cfg, err := h.requireConfigurer()
	if err != nil {
		return "", err
	}
	return cfg.Dir(path), nil
}

// OSKind returns the host OS kind via the resolved configurer.
func (h *ZarfHost) OSKind() (string, error) {
	cfg, err := h.requireConfigurer()
	if err != nil {
		return "", err
	}
	return cfg.OSKind(), nil
}

// Arch returns the host architecture, caching the result in metadata
func (h *ZarfHost) Arch() (string, error) {
	if h.Metadata.Arch != "" {
		return h.Metadata.Arch, nil
	}
	if h.Configurer == nil {
		return "", fmt.Errorf("host configurer is not resolved")
	}
	arch, err := h.Configurer.Arch(h)
	if err != nil {
		return "", fmt.Errorf("failed to detect host architecture: %w", err)
	}
	h.Metadata.Arch = arch
	return arch, nil
}

// Touch updates file modification timestamps via the resolved configurer.
func (h *ZarfHost) Touch(path string, modTime time.Time, opts ...exec.Option) error {
	cfg, err := h.requireConfigurer()
	if err != nil {
		return err
	}
	return cfg.Touch(h, path, modTime, opts...)
}

// DeleteFile removes a file via the resolved configurer.
func (h *ZarfHost) DeleteFile(path string) error {
	cfg, err := h.requireConfigurer()
	if err != nil {
		return err
	}
	return cfg.DeleteFile(h, path)
}

func (h *ZarfHost) KubeRole() string {
	switch h.Role {
	case ROLE_CONTROLLER_WORKER, ROLE_SINGLE:
		return ROLE_CONTROLLER
	default:
		return h.Role
	}
}

// IsController returns true for controller and controller+worker roles
func (h *ZarfHost) IsController() bool {
	return h.Role == ROLE_CONTROLLER || h.Role == ROLE_CONTROLLER_WORKER || h.Role == ROLE_SINGLE
}

// ServiceName returns correct service name
func (h *ZarfHost) ServiceName() string {
	switch h.Role {
	case ROLE_CONTROLLER, ROLE_CONTROLLER_WORKER, ROLE_SINGLE:
		val, err := h.Configurer.GetDistroService(ROLE_CONTROLLER)
		if err != nil {
			return ROLE_ERROR
		}
		return val
	default:
		val, err := h.Configurer.GetDistroService(ROLE_WORKER)
		if err != nil {
			return ROLE_ERROR
		}
		return val
	}
}

// ResolveConfigurer assigns a rig-style configurer to the Host (see configurer/)
func (h *ZarfHost) ResolveConfigurer() error {
	bf, err := registry.GetOSModuleBuilder(*h.OSVersion)
	if err != nil {
		return err
	}

	if c, ok := bf().(configurer.Configurer); ok {
		h.Configurer = c

		return nil
	}

	return fmt.Errorf("unsupported OS")
}

// FileChanged returns true when a remote file has different size or mtime compared to local
// or if an error occurs
func (h *ZarfHost) FileChanged(lpath, rpath string) bool {
	lstat, err := gos.Stat(lpath)
	if err != nil {
		log.Debugf("%s: local stat failed: %s", h, err)
		return true
	}
	rstat, err := h.Configurer.Stat(h, rpath, exec.Sudo(h))
	if err != nil {
		log.Debugf("%s: remote stat failed: %s", h, err)
		return true
	}

	if lstat.Size() != rstat.Size() {
		log.Debugf("%s: file sizes for %s differ (%d vs %d)", h, lpath, lstat.Size(), rstat.Size())
		return true
	}

	if !lstat.ModTime().Equal(rstat.ModTime()) {
		log.Debugf("%s: file modtimes for %s differ (%s vs %s)", h, lpath, lstat.ModTime(), rstat.ModTime())
		return true
	}

	return false
}

// WriteFile writes file to host with given contents. Do not use for large files.
func (h *ZarfHost) WriteFile(path string, data string, permissions string) error {
	cfg, err := h.requireConfigurer()
	if err != nil {
		return err
	}
	return cfg.WriteFile(h, path, data, permissions)
}

func (h *ZarfHost) ReadFile(path string) (string, error) {
	cfg, err := h.requireConfigurer()
	if err != nil {
		return "", err
	}
	return cfg.ReadFile(h, path)
}

func (h *ZarfHost) FileExist(path string) bool {
	cfg, err := h.requireConfigurer()
	if err != nil {
		return false
	}
	return cfg.FileExist(h, path)
}

// CheckHTTPStatus will perform a web request to the url and return an error if the http status is not the expected
func (h *ZarfHost) CheckHTTPStatus(url string, expected ...int) error {
	status, err := h.Configurer.HTTPStatus(h, url)
	if err != nil {
		return err
	}

	if slices.Contains(expected, status) {
		return nil
	}

	return fmt.Errorf("expected response code %d but received %d", expected, status)
}
