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

package os

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"

	"github.com/colonel-byte/cargoship/src/internal/riglogger"
	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/sh"
)

const (
	// BinaryPath key for the binary path for the distro engine
	BinaryPath = "BinaryPath"
	// ConfigPath key for the config directory for the distro engine
	ConfigPath = "ConfigPath"
	// JoinTokenPath key for the join token for the cluster
	JoinTokenPath = "JoinTokenPath"
	// DataDirDefaultPath key for the data directory for the distro engine
	DataDirDefaultPath = "DataDirDefaultPath"
)

// Linux is a base module for various linux OS support packages
type Linux struct {
	paths    map[string]string
	services map[string]string
}

// Kind returns the general OS family identifier for Linux hosts
func (l *Linux) Kind() string {
	return "linux"
}

// OSKind returns the identifier for Linux hosts
func (l *Linux) OSKind() string {
	return "linux"
}

// Dir returns the directory part of a path
func (l *Linux) Dir(p string) string {
	return path.Dir(p)
}

// Hostname resolves the short hostname
func (l *Linux) Hostname(h Host) string {
	n, err := h.FS().Hostname()
	if err != nil {
		riglogger.Logger().Debug("failed to resolve short hostname", "error", err)
	}
	return n
}

// LongHostname resolves the FQDN (long) hostname
func (l *Linux) LongHostname(h Host) string {
	n, err := h.FS().LongHostname()
	if err != nil {
		riglogger.Logger().Debug("failed to resolve long hostname", "error", err)
	}
	return n
}

// CTLLockFilePath returns a path to a lock file
func (l *Linux) CTLLockFilePath(h Host) string {
	if h.Sudo().FS().FileExist("/run/lock") {
		return "/run/lock/ctl"
	}

	return "/tmp/ctl.lock"
}

const sbinPath = `PATH=/usr/local/sbin:/usr/sbin:/sbin:$PATH`

// PrivateInterface tries to find a private network interface
func (l *Linux) PrivateInterface(h Host) (string, error) {
	output, err := h.ExecOutput(fmt.Sprintf(`%s; (ip route list scope global | grep -E "\b(172|10|192\.168)\.") || (ip route list | grep -m1 default)`, sbinPath))
	if err == nil {
		re := regexp.MustCompile(`\bdev (\w+)`)
		match := re.FindSubmatch([]byte(output))
		if len(match) > 0 {
			return string(match[1]), nil
		}
		err = fmt.Errorf("can't find 'dev' in output")
	}

	return "", fmt.Errorf("failed to detect a private network interface, define the host privateInterface manually (%s)", err.Error())
}

// PrivateAddress resolves internal ip from private interface
func (l *Linux) PrivateAddress(h Host, iface, publicip string) (string, error) {
	output, err := h.ExecOutput(fmt.Sprintf("%s ip -o addr show dev %s scope global", sbinPath, iface))
	if err != nil {
		return "", fmt.Errorf("failed to find private interface with name %s: %s. Make sure you've set correct 'privateInterface' for the host in config", iface, output)
	}

	lines := strings.SplitSeq(output, "\n")
	for line := range lines {
		items := strings.Fields(line)
		if len(items) < 4 {
			continue
		}
		// When subnet mask is 255.255.255.255, CIDR notation is not /32, but it is omitted instead.
		index := strings.Index(items[3], "/")
		addr := items[3]
		if index >= 0 {
			addr = items[3][:index]
		}
		if len(strings.Split(addr, ".")) == 4 {
			if publicip != addr {
				return addr, nil
			}
		}
	}

	return "", fmt.Errorf("not found")
}

// UpdateEnvironment upserts the given key-value pairs into /etc/environment
// (replacing any existing line for the same key) and exports them into the
// current shell environment.
func (l *Linux) UpdateEnvironment(h Host, env map[string]string) error {
	fsys := h.Sudo().FS()
	for k, v := range env {
		if strings.ContainsAny(k, "=\n") {
			return fmt.Errorf("invalid environment variable key %q: must not contain '=' or newline", k)
		}
		if strings.ContainsRune(v, '\n') {
			return fmt.Errorf("invalid environment variable value for key %q: must not contain newline", k)
		}
		patch := remotefs.ReplaceOrAppend(remotefs.ByPrefix(k+"="), fmt.Sprintf("%s=%s", k, v))
		if err := remotefs.PatchFile(fsys, "/etc/environment", []remotefs.Patch{patch}, remotefs.WithCreate(fs.FileMode(0o644))); err != nil {
			return fmt.Errorf("failed to update /etc/environment: %w", err)
		}
	}

	// Export the values into the current session environment using the
	// in-memory values with proper shell escaping. Reading them back from
	// /etc/environment and running 'export "$pair"' would re-export any
	// surrounding quote or escape characters literally.
	var export strings.Builder
	for k, v := range env {
		fmt.Fprintf(&export, "export %s=%s\n", k, sh.Quote(v))
	}
	if export.Len() > 0 {
		if err := h.Sudo().Exec(export.String()); err != nil {
			return fmt.Errorf("failed to update environment: %w", err)
		}
	}
	return nil
}

// GetDistroService returns the name of the service for the current distro.
// common key values are, "controller" and "agent"
func (l *Linux) GetDistroService(key string) (string, error) {
	if l.services == nil {
		return "", fmt.Errorf("the services map is not populated")
	}
	if val, ok := l.services[key]; ok {
		return val, nil
	}
	return "", fmt.Errorf("the service %s does not exist in the service map", key)
}

// SetPath adds a key value to the paths string map.
// TODO add ref to what the paths string map does....
func (l *Linux) SetPath(key, value string) {
	l.paths[key] = value
}
