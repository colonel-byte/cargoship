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
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	gos "os"
	"slices"
	"time"

	"github.com/colonel-byte/cargoship/src/types/os"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/os/registry"
)

const (
	// RoleController string enum
	RoleController = "controller"
	// RoleControllerWorker string enum
	RoleControllerWorker = "controller+worker"
	// RoleSingle string enum
	RoleSingle = "single"
	// RoleWorker string enum
	RoleWorker = "worker"
	// RoleError string enum
	RoleError = "error"
)

// ErrCommandFailed is returned when a command fails
var ErrCommandFailed = errors.New("command failed")

// ZarfHost is a remote connection to a node
type ZarfHost struct {
	rig.Connection `json:",inline"`
	//keep-sorted start
	Environment      map[string]string  `json:"environment,omitempty"`
	Files            []ZarfClusterFiles `json:"files,omitempty"`
	Hostname         string             `json:"hostname,omitempty"`
	NodeLabels       map[string]string  `json:"labels,omitempty"`
	NodeTaints       []string           `json:"taints,omitempty"`
	Ports            []ZarfHostPort     `json:"ports,omitempty" xml:"port"`
	PrivateAddress   string             `json:"privateAddress,omitempty"`
	PrivateInterface string             `json:"privateInterface,omitempty"`
	Profile          string             `json:"profile,omitempty"`
	Role             string             `json:"role" jsonschema:"enum=controller,enum=controller+worker,enum=single,enum=worker"`
	//keep-sorted end
	Configurer os.Configurer    `json:"-"`
	Metadata   ZarfHostMetadata `json:"-"`
}

// ZarfHostPort ports that should be opened on the public side of the firewall
type ZarfHostPort struct {
	Protocol string `json:"protocol" xml:"protocol,attr" jsonschema:"enum=tcp,enum=udp"`
	Port     string `json:"port" xml:"port,attr" jsonschema:"oneof_type=string;integer"`
}

// ZarfHostMetadata runtime discovered values
type ZarfHostMetadata struct {
	//keep-sorted start
	Arch           string
	BinaryTempFile []string
	DistroVersion  string
	EngineUploaded bool
	ExistingConfig string
	Hostname       string
	Install        func(context.Context, *ZarfHost) error
	Installed      bool
	IsLeader       bool
	MachineID      string
	NeedsUpgrade   bool
	NewConfig      string
	Ready          bool
	//keep-sorted end
}

func (h *ZarfHost) requireConfigurer() (os.Configurer, error) {
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

// KubeRole of the role host
func (h *ZarfHost) KubeRole() string {
	switch h.Role {
	case RoleControllerWorker, RoleSingle:
		return RoleController
	default:
		return h.Role
	}
}

// IsController returns true for controller and controller+worker roles
func (h *ZarfHost) IsController() bool {
	return h.Role == RoleController || h.Role == RoleControllerWorker || h.Role == RoleSingle
}

// ServiceName returns correct service name
func (h *ZarfHost) ServiceName() string {
	switch h.Role {
	case RoleController, RoleControllerWorker, RoleSingle:
		val, err := h.Configurer.GetDistroService(RoleController)
		if err != nil {
			return RoleError
		}
		return val
	default:
		val, err := h.Configurer.GetDistroService(RoleWorker)
		if err != nil {
			return RoleError
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

	if c, ok := bf().(os.Configurer); ok {
		h.Configurer = c

		return nil
	}

	return fmt.Errorf("unsupported OS")
}

// FileChanged returns true when a remote file has a different sha256 checksum or if an error occurs
func (h *ZarfHost) FileChanged(lpath, rpath string) bool {
	file, err := gos.Open(lpath)
	if err != nil {
		return true
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Warnf("got the following error: %w", err)
		}
	}()
	lsha := sha256.New()
	if _, err = io.Copy(lsha, file); err != nil {
		return true
	}
	rsha, err := h.Configurer.Sha256sum(h, rpath, exec.Sudo(h))
	if err != nil {
		return true
	}

	sum := fmt.Sprintf("%x", lsha.Sum(nil))
	if sum != rsha {
		log.Debugf("%s: file sha256 for %s differ (%s vs %s)", h, lpath, sum, rsha)
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

// ReadFile read the contents of a file, if it exists, or returns an error
func (h *ZarfHost) ReadFile(path string) (string, error) {
	cfg, err := h.requireConfigurer()
	if err != nil {
		return "", err
	}
	return cfg.ReadFile(h, path)
}

// FileExist if a file exists on the host
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
