// Copyright 2023 k0sctl authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
//
// Modifications Copyright 2026 colonel-byte.
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
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
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
	// RoleController marks a host as a control-plane node.
	RoleController = "controller"
	// RoleControllerWorker marks a host as both a control-plane node and a worker node.
	RoleControllerWorker = "controller+worker"
	// RoleSingle marks a host as a single-node cluster: both control plane and worker on one host.
	RoleSingle = "single"
	// RoleWorker marks a host as a worker node.
	RoleWorker = "worker"
	// RoleError is a sentinel value returned when a role or service lookup fails.
	RoleError = "error"
)

// ErrCommandFailed is returned when a command fails
var ErrCommandFailed = errors.New("command failed")

// ZarfHost is a remote connection to a node
type ZarfHost struct {
	// Connection embeds rig's connection type. It gives ZarfHost multi-protocol connectivity to a remote host.
	rig.Connection `json:",inline"`
	// Environment maps environment variables cargoship sets on the host.
	Environment map[string]string `json:"environment,omitempty"`
	// Files lists files cargoship uploads to the host.
	Files []ZarfClusterFiles `json:"files,omitempty"`
	// Hostname overrides the discovered name of the node.
	Hostname string `json:"hostname,omitempty"`
	// PrivateAddress overrides the discovered private address of the node.
	PrivateAddress string `json:"privateAddress,omitempty"`
	// PrivateInterface overrides the discovered private interface of the node.
	PrivateInterface string `json:"privateInterface,omitempty"`
	// Profile selects a profile by name from the cluster config's Profiles map.
	Profile string `json:"profile,omitempty"`
	// Role sets the node role when cargoship adds this host to the cluster. It must be controller or worker.
	Role string `json:"role" jsonschema:"required,enum=controller,enum=worker"`
	// Host holds the host-level configuration overrides for this node.
	Host ZarfHostConfig `json:"host,omitempty"`
	// Engine holds the node label and taint overrides for this node.
	Engine ZarfHostEngine `json:"engine,omitempty"`
	// Configurer is the per-host operations implementation cargoship uses to manage this host.
	Configurer os.Configurer `json:"-"`
	// Metadata holds values cargoship discovers about this host at runtime.
	Metadata ZarfHostMetadata `json:"-"`
}

// ZarfHostConfig defines the configuration for a specific host, including
// firewall policies and the ports cargoship opens on the node.
type ZarfHostConfig struct {
	// Policy maps a policy name to a firewalld policy that allows traffic from one interface to another.
	Policy map[string]ZarfFirewallPolicyConfig `json:"policy,omitempty"`
	// Ports lists the ports and protocols cargoship opens on the node.
	Ports []ZarfHostPort `json:"ports,omitempty" xml:"port"`
}

// Merge copies Policy and Ports from update into c, for whichever of those fields are empty in c.
func (c *ZarfHostConfig) Merge(update ZarfHostConfig) {
	if len(c.Policy) == 0 && len(update.Policy) > 0 {
		c.Policy = make(map[string]ZarfFirewallPolicyConfig)
		maps.Copy(c.Policy, update.Policy)
	}
	if len(c.Ports) == 0 && len(update.Ports) > 0 {
		for _, p := range update.Ports {
			if slices.Contains(c.Ports, p) {
				continue
			}
			c.Ports = append(c.Ports, p)
		}
	}
}

// ZarfHostEngine defines configuration options for node-level metadata,
// specifically Kubernetes node labels and taints applied to a cluster host.
type ZarfHostEngine struct {
	// NodeLabels maps Kubernetes node label keys to their values.
	NodeLabels map[string]string `json:"labels,omitempty"`
	// NodeTaints lists the Kubernetes node taints cargoship applies to the node.
	NodeTaints []string `json:"taints,omitempty"`
}

// Merge copies NodeLabels and NodeTaints from update into c, for whichever of those fields are empty in c.
func (c *ZarfHostEngine) Merge(update ZarfHostEngine) {
	if len(c.NodeLabels) == 0 && len(update.NodeLabels) > 0 {
		c.NodeLabels = make(map[string]string)
		maps.Copy(c.NodeLabels, update.NodeLabels)
	}
	if len(c.NodeTaints) == 0 && len(update.NodeTaints) > 0 {
		for _, p := range update.NodeTaints {
			if slices.Contains(c.NodeTaints, p) {
				continue
			}
			c.NodeTaints = append(c.NodeTaints, p)
		}
	}
}

// ZarfHostPort is a port cargoship opens on the public side of the firewall.
type ZarfHostPort struct {
	// Protocol is the type of allowed traffic.
	Protocol string `json:"protocol" xml:"protocol,attr" jsonschema:"enum=tcp,enum=udp"`
	// Port is the port number, or port range, cargoship opens.
	Port string `json:"port" xml:"port,attr" jsonschema:"oneof_type=string;integer"`
}

// ZarfFirewallPolicyConfig configures a firewalld policy that opens ports from one zone to another.
type ZarfFirewallPolicyConfig struct {
	// XMLName sets the element name cargoship uses when it marshals this policy to firewalld XML.
	XMLName xml.Name `xml:"policy" json:"-"`
	// Short is a human-readable description of the policy.
	Short string `xml:"short,omitempty" json:"-"`
	// Target is the action taken on traffic that matches the policy.
	Target string `xml:"target,attr,omitempty" json:"target" jsonschema:"enum=CONTINUE,enum=ACCEPT,enum=REJECT,enum=DROP"`
	// Ingress is the zone the policy allows traffic from.
	Ingress ZarfFirewallZone `xml:"ingress-zone"`
	// Egress is the zone the policy allows traffic to.
	Egress ZarfFirewallZone `xml:"egress-zone"`
	// Ports lists the ports this policy allows.
	Ports []ZarfFirewallPort `xml:"port,omitempty" json:"ports,omitempty"`
}

// ZarfFirewallZone is the name of either the ingress or egress zone for a policy.
type ZarfFirewallZone struct {
	// Name identifies the firewalld zone.
	Name string `xml:"name,attr" jsonschema:"example=trusted,example=public"`
}

// ZarfFirewallPort defines a port allowed through the firewalld policy.
type ZarfFirewallPort struct {
	// Protocol is the type of allowed traffic.
	Protocol string `xml:"protocol,attr" json:"protocol" jsonschema:"enum=tcp,enum=udp,enum=sctp,enum=dccp"`
	// Port is the port number, or port range, cargoship opens.
	Port string `xml:"port,attr" json:"port" jsonschema:"oneof_type=string;integer"`
}

// ZarfHostMetadata holds values cargoship discovers about a host at runtime.
type ZarfHostMetadata struct {
	// Arch is the CPU architecture detected on the host.
	Arch string
	// BinaryTempFile lists temporary paths on the host cargoship uses to stage the engine binary during install.
	BinaryTempFile []string
	// DistroVersion is the version of the distro engine detected on the host.
	DistroVersion string
	// EngineUploaded indicates whether cargoship has already uploaded the distro engine binary to the host.
	EngineUploaded bool
	// ExistingConfig is the engine configuration currently present on the host.
	ExistingConfig string
	// Hostname is the hostname the host reports.
	Hostname string
	// Install is the function cargoship calls to install the distro engine on the host.
	Install func(context.Context, *ZarfHost) error
	// Installed indicates whether a distro engine is already installed on the host.
	Installed bool
	// IsLeader indicates whether this host is the cluster's control-plane leader.
	IsLeader bool
	// MachineID identifies the node to the distro engine.
	MachineID string
	// ModulesAdded indicates whether cargoship added a new kernel module to the host.
	ModulesAdded bool
	// NeedsUpgrade indicates whether the host needs the distro engine upgraded.
	NeedsUpgrade bool
	// NewConfig is the engine configuration cargoship will write to the host.
	NewConfig string
	// Ready indicates whether the distro service is up and running.
	Ready bool
	// UploadedFiles lists "category\tpath" entries cargoship uploaded to this host during the
	// current run. It is used to detect files a previous version left behind that the current
	// upload no longer produces, e.g. an engine binary renamed by a version bump.
	UploadedFiles []string
}

// requireConfigurer returns the resolved configurer for h, or an error if none has been resolved yet.
func (h *ZarfHost) requireConfigurer() (os.Configurer, error) {
	if h.Configurer == nil {
		return nil, fmt.Errorf("%s: host configurer is not resolved", h)
	}
	return h.Configurer, nil
}

// String returns the connection string
func (h *ZarfHost) String() string {
	return h.Connection.String()
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

// KubeRole returns the Kubernetes role for this host. It maps controller+worker and single to controller.
func (h *ZarfHost) KubeRole() string {
	switch h.Role {
	case RoleControllerWorker, RoleSingle:
		return RoleController
	default:
		return h.Role
	}
}

// IsController returns true for the controller, controller+worker, and single roles.
func (h *ZarfHost) IsController() bool {
	return h.Role == RoleController || h.Role == RoleControllerWorker || h.Role == RoleSingle
}

// ServiceName returns the name of the distro service that runs on this host.
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

// ResolveConfigurer detects the host OS version and assigns the matching configurer to Configurer.
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

// FileChanged compares the local file at lpath to the remote file at rpath by sha256 checksum.
// It returns true if the checksums differ or if either checksum cannot be computed.
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

// WriteFile writes data to path on the host. Do not use this for large files.
func (h *ZarfHost) WriteFile(path string, data string, permissions string) error {
	cfg, err := h.requireConfigurer()
	if err != nil {
		return err
	}
	return cfg.WriteFile(h, path, data, permissions)
}

// ReadFile returns the contents of path on the host, or an error if the file does not exist.
func (h *ZarfHost) ReadFile(path string) (string, error) {
	cfg, err := h.requireConfigurer()
	if err != nil {
		return "", err
	}
	return cfg.ReadFile(h, path)
}

// FileExist returns true if path exists on the host.
func (h *ZarfHost) FileExist(path string) bool {
	cfg, err := h.requireConfigurer()
	if err != nil {
		return false
	}
	return cfg.FileExist(h, path)
}

// CheckHTTPStatus requests url and returns an error if the response status is not one of expected.
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
