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
	"time"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
)

// Configurer defines the per-host operations required for managing a host.
type Configurer interface {
	// Arch returns the host processor architecture in the format engine expects it
	Arch(os.Host) (string, error)
	// Base returns the base part of a path
	Base(string) string
	// CTLLockFilePath returns a path to a lock file
	CTLLockFilePath(h os.Host) string
	// CheckPrivilege checks if the current user has root privileges
	CheckPrivilege(os.Host) error
	// Chmod updates permissions of a path
	Chmod(os.Host, string, string, ...exec.Option) error
	// Chown sets owner for a file or directory
	Chown(os.Host, string, string, ...exec.Option) error
	// CleanupServiceEnvironment updates environment variables for a service
	CleanupServiceEnvironment(os.Host, string) error
	// CommandExist returns true if the command exists
	CommandExist(os.Host, string) bool
	// DaemonReload performs an init system config reload
	DaemonReload(os.Host) error
	// DeleteDir removes a directory
	DeleteDir(os.Host, string, ...exec.Option) error
	// DeleteFile deletes a file from the host.
	DeleteFile(os.Host, string) error
	// Dir returns the directory part of a path
	Dir(string) string
	// DownloadURL performs a download from a URL on the host
	DownloadURL(os.Host, string, string, ...exec.Option) error
	// EnableService enables a service on the host
	EnableService(os.Host, string) error
	// FileContains returns true if a file contains the substring
	FileContains(os.Host, string, string) bool
	// FileExist checks if a file exists on the host
	FileExist(os.Host, string) bool
	// GetDistroService returns the name of the service for the current distro.
	// common key values are, "controller" and "agent"
	GetDistroService(string) (string, error)
	// HTTPStatus makes a HTTP GET request to the url and returns the status code or an error
	HTTPStatus(os.Host, string) (int, error)
	// HostPath returns the given path unchanged for linux hosts
	HostPath(string) string
	// Hostname resolves the short hostname
	Hostname(os.Host) string
	// InstallPackage installs packages
	InstallPackage(os.Host, ...string) error
	// OSKind returns the identifier for Linux hosts
	Kind() string
	// LongHostname resolves the FQDN (long) hostname
	LongHostname(os.Host) string
	// LookPath behaves similarly to exec.LookPath but resolves the binary on the remote host
	LookPath(os.Host, string) (string, error)
	// MkDir creates a directory (including intermediate directories)
	MkDir(os.Host, string, ...exec.Option) error
	// MoveFile moves a file on the host
	MoveFile(os.Host, string, string) error
	// OSKind returns the identifier for Linux hosts
	OSKind() string
	// PrivateAddress resolves internal ip from private interface
	PrivateAddress(os.Host, string, string) (string, error)
	// PrivateInterface tries to find a private network interface
	PrivateInterface(os.Host) (string, error)
	// Quote wraps shellescape.Quote for consumers that need OS-aware escaping
	Quote(string) string
	// ReadFile reads a files contents from the host.
	ReadFile(os.Host, string) (string, error)
	// Reboot executes the reboot command
	Reboot(h os.Host) error
	// RestartService restarts a service on the host
	RestartService(os.Host, string) error
	// SELinuxEnabled is true when SELinux is enabled
	SELinuxEnabled(os.Host) bool
	// ServiceIsRunning returns true if the service is running on the host
	ServiceIsRunning(os.Host, string) bool
	// ServiceScriptPath returns the service definition file path on the host
	ServiceScriptPath(os.Host, string) (string, error)
	// SetPath adds a key value to the paths string map.
	SetPath(string, string)
	// Sha256sum calculates the sha256 checksum of a file
	Sha256sum(h os.Host, path string, opts ...exec.Option) (string, error)
	// StartService starts a service on the host
	StartService(os.Host, string) error
	// Stat gets file / directory information
	Stat(os.Host, string, ...exec.Option) (*os.FileInfo, error)
	// StopService stops a service on the host
	StopService(os.Host, string) error
	// SystemTime returns the system time as UTC reported by the OS or an error if this fails
	SystemTime(os.Host) (time.Time, error)
	// TempDir returns a temp dir path
	TempDir(os.Host) (string, error)
	// TempFile returns a temp file path
	TempFile(os.Host) (string, error)
	// Touch updates a file's last modified time. It creates a new empty file if it
	// didn't exist prior to the call to Touch.
	Touch(os.Host, string, time.Time, ...exec.Option) error
	// UninstallPackage installs packages
	UninstallPackage(os.Host, ...string) error
	// UpdateEnvironment updates the hosts's environment variables
	UpdateEnvironment(os.Host, map[string]string) error
	// UpdateServiceEnvironment updates environment variables for a service
	UpdateServiceEnvironment(os.Host, string, map[string]string) error
	// UpsertFile creates a file in path with content only if the file does not exist already
	UpsertFile(os.Host, string, string) error
	// WriteFile writes file to host with given contents. Do not use for large files.
	WriteFile(os.Host, string, string, string) error
}

// HostValidator allows a Configurer to implement host-specific validation logic.
type HostValidator interface {
	ValidateHost(os.Host) error
}
