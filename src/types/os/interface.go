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

// Package os is for running commands on a remote host
package os

import (
	rig "github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/remotefs"
)

// Host is the interface that Configurer methods use to interact with a
// remote host. It is satisfied by *rig.Client, and therefore by any type
// that embeds rig.ClientWithConfig (such as cluster.ZarfHost), since those
// methods are promoted.
type Host interface {
	cmd.SimpleRunner
	// Sudo returns a memoized clone of the client whose runner wraps every
	// command with the detected privilege escalation mechanism.
	Sudo() *rig.Client
	// FS returns a filesystem interface for accessing files on the host.
	FS() remotefs.FS
}

// Configurer defines the per-host operations required for managing a host.
// Most filesystem, service, and OS-detection operations are handled
// directly via the rig v2 Host interface (FS(), Sudo(), OS()) rather than
// through this interface -- Configurer only carries what is genuinely
// distro-specific.
type Configurer interface {
	// Kind returns the general OS family identifier (e.g. "linux")
	Kind() string
	// OSKind returns the identifier for Linux hosts
	OSKind() string
	// Dir returns the directory part of a path
	Dir(string) string
	// GetDistroService returns the name of the service for the current distro.
	// common key values are, "controller" and "agent"
	GetDistroService(string) (string, error)
	// SetPath adds a key value to the paths string map.
	SetPath(string, string)
	// UpdateEnvironment updates the hosts's environment variables
	UpdateEnvironment(Host, map[string]string) error
	// Hostname resolves the short hostname
	Hostname(Host) string
	// LongHostname resolves the FQDN (long) hostname
	LongHostname(Host) string
	// InstallPackage installs packages
	InstallPackage(Host, ...string) error
	// UninstallPackage uninstalls packages
	UninstallPackage(Host, ...string) error
	// CTLLockFilePath returns a path to a lock file
	CTLLockFilePath(Host) string
	// PrivateInterface tries to find a private network interface
	PrivateInterface(Host) (string, error)
	// PrivateAddress resolves internal ip from private interface
	PrivateAddress(Host, string, string) (string, error)
}

// HostValidator allows a Configurer to implement host-specific validation logic.
type HostValidator interface {
	ValidateHost(Host) error
}
