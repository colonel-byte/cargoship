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
	RootCmdUse   = "zarf-distro COMMAND"
	RootCmdShort = "CLI for Zarf Distro installs"

	RootGroupPackageTitle = "Package Commands:"
	RootGroupPackageID    = "package"

	RootGroupInstallTitle = "Install Commands:"
	RootGroupInstallID    = "install"

	CmdViperErrLoadingConfigFile = "failed to load config file"
	CmdDistroCreateShort         = "Creates a Zarf Distro Package from a given directory or the current director"

	CmdPackageFlagConcurrency  = "Number of concurrent layer operations when pulling or pushing images or packages to/from OCI registries."
	CmdPackageCreateFlagOutput = "Specify the output (either a directory or an oci:// URL) for the created Zarf distro package"
	RootCmdFlagLogLevel        = "Log level when running zarf-distro. Valid options are: warn, info, debug, trace"
)
