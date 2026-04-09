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

package os

import (
	"time"

	"github.com/colonel-byte/zarf-distro/src/types/distro"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
)

// Configurer defines the per-host operations required for managing a host.
type Configurer interface {
	//keep-sorted start
	Arch(os.Host) (string, error)
	Base(string) string
	CTLLockFilePath(h os.Host) string
	CheckPrivilege(os.Host) error
	Chmod(os.Host, string, string, ...exec.Option) error
	Chown(os.Host, string, string, ...exec.Option) error
	CleanupServiceEnvironment(os.Host, string) error
	CommandExist(os.Host, string) bool
	ConfigureDistro(distro.Distro)
	ConfigureDistroServices(map[string]string)
	DaemonReload(os.Host) error
	DeleteDir(os.Host, string, ...exec.Option) error
	DeleteFile(os.Host, string) error
	Dir(string) string
	DownloadURL(os.Host, string, string, ...exec.Option) error
	EnableService(os.Host, string) error
	FileContains(os.Host, string, string) bool
	FileExist(os.Host, string) bool
	GetDistroService(string) (string, error)
	GetSysctlValue(os.Host, string) (string, error)
	HTTPStatus(os.Host, string) (int, error)
	HostPath(string) string
	Hostname(os.Host) string
	InstallPackage(os.Host, ...string) error
	Kind() string
	LookPath(os.Host, string) (string, error)
	MachineID(os.Host) (string, error)
	MkDir(os.Host, string, ...exec.Option) error
	MoveFile(os.Host, string, string) error
	OSKind() string
	PrivateAddress(os.Host, string, string) (string, error)
	PrivateInterface(os.Host) (string, error)
	Quote(string) string
	ReadFile(os.Host, string) (string, error)
	RestartService(os.Host, string) error
	SELinuxEnabled(os.Host) bool
	ServiceIsRunning(os.Host, string) bool
	ServiceScriptPath(os.Host, string) (string, error)
	SetPath(string, string)
	SetSysctlValue(os.Host, string, string) error
	StartService(os.Host, string) error
	Stat(os.Host, string, ...exec.Option) (*os.FileInfo, error)
	StopService(os.Host, string) error
	SystemTime(os.Host) (time.Time, error)
	TempDir(os.Host) (string, error)
	TempFile(os.Host) (string, error)
	Touch(os.Host, string, time.Time, ...exec.Option) error
	UpdateEnvironment(os.Host, map[string]string) error
	UpdateServiceEnvironment(os.Host, string, map[string]string) error
	UpsertFile(os.Host, string, string) error
	WriteFile(os.Host, string, string, string) error
	//keep-sorted end
}

// HostValidator allows a Configurer to implement host-specific validation logic.
type HostValidator interface {
	ValidateHost(os.Host) error
}
