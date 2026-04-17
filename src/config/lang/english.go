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

package lang

const (
	//keep-sorted start
	CmdDistroCreateShort            = "Creates a Zarf Distro Package from a given directory or the current director"
	CmdInstallFirewallUpdate        = "Whether to update all the host nodes firewall configuration."
	CmdInstallFlagConcurrency       = "Maximum number of hosts to configure in parallel, set to 0 for unlimited."
	CmdInstallFlagConfig            = "Config file used to bootstrap a cluster."
	CmdInstallFlagWorkerConcurrency = "Maximum number of workers that will be installed or updated in parallel, set to 0 for unlimited."
	CmdInstallHostUpdate            = "Whether to update all the host nodes etc/hosts file."
	CmdPackageCreateFlagOutput      = "Specify the output (either a directory or an oci:// URL) for the created Zarf distro package"
	CmdPackageFlagConcurrency       = "Number of concurrent layer operations when pulling or pushing images or packages to/from OCI registries."
	CmdViperErrLoadingConfigFile    = "failed to load config file"
	RootCmdFlagLogFormat            = "Select a logging format. Defaults to 'console'. Valid options are: 'console', 'json', 'dev'."
	RootCmdFlagLogLevel             = "Log level when running zarf-distro. Valid options are: warn, info, debug, trace"
	RootCmdFlagNoColor              = "Disable terminal color codes in logging and stdout prints."
	RootCmdShort                    = "CLI for Zarf Distro installs"
	RootCmdUse                      = "zarf-distro COMMAND"
	RootGroupInstallID              = "install"
	RootGroupInstallTitle           = "Install Commands:"
	RootGroupPackageID              = "package"
	RootGroupPackageTitle           = "Package Commands:"
	//keep-sorted end
)
