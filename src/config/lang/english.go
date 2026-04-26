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

// Package lang holds the cli helping text
package lang

const (
	// CmdDistroCreateShort create short
	CmdDistroCreateShort = "Creates a Zarf Distro Package from a given directory or the current director"
	// CmdDistroApplyShort create short
	CmdDistroApplyShort = "Apply a config file to bootstrap and upgrade a cluster"
	// CmdDistroResetShort create short
	CmdDistroResetShort = "Reset a cluster, stopping, uninstalling, and removing all data for a engine"
	// CmdInstallFapolicydUpdate install flag fapolicyd
	CmdInstallFapolicydUpdate = "Whether to update all the host nodes fapolicyd configuration."
	// CmdInstallFirewallUpdate install flag firewall
	CmdInstallFirewallUpdate = "Whether to update all the host nodes firewall configuration."
	// CmdInstallFlagConcurrency install flag concurrency
	CmdInstallFlagConcurrency = "Maximum number of hosts to configure in parallel, set to 0 for unlimited."
	// CmdInstallFlagConfig install flag config
	CmdInstallFlagConfig = "Config file used to bootstrap a cluster."
	// CmdInstallFlagResetDistro install flag config
	CmdInstallFlagResetDistro = "What type of distro that will be reset. Valid options are: 'rke2', 'k3s'."
	// CmdInstallFlagConfirm install flag confirm
	CmdInstallFlagConfirm = "Confirm whether if to proceed with the install"
	// CmdInstallFlagTimeout install flag timeout
	CmdInstallFlagTimeout = "Set the timeout for how long functions will last."
	// CmdInstallFlagWorkerConcurrency install flag worker concurrency
	CmdInstallFlagWorkerConcurrency = "Maximum number of workers that will be installed or updated in parallel, set to 0 for unlimited."
	// CmdInstallHostUpdate install flag host
	CmdInstallHostUpdate = "Whether to update all the host nodes etc/hosts file."
	// CmdPackageCreateFlagOutput create flag output
	CmdPackageCreateFlagOutput = "Specify the output (either a directory or an oci:// URL) for the created Zarf distro package"
	// CmdPackageFlagConcurrency deploy flag concurrency
	CmdPackageFlagConcurrency = "Number of concurrent layer operations when pulling or pushing images or packages to/from OCI registries."
	// CmdVersionLong version long
	CmdVersionLong = "Displays the version of the release that the current binary was built from."
	// CmdVersionShort version short
	CmdVersionShort = "Shows the version of the running binary"
	// CmdViperErrLoadingConfigFile error text
	CmdViperErrLoadingConfigFile = "failed to load config file"
	// RootCmdFlagLogFormat log format
	RootCmdFlagLogFormat = "Select a logging format. Defaults to 'console'. Valid options are: 'console', 'json', 'dev'."
	// RootCmdFlagLogLevel log level
	RootCmdFlagLogLevel = "Log level when running cargoship. Valid options are: warn, info, debug, trace"
	// RootCmdFlagNoColor no color
	RootCmdFlagNoColor = "Disable terminal color codes in logging and stdout prints."
	// RootCmdShort root short
	RootCmdShort = "CLI for cargoship installs"
	// RootCmdUse root use
	RootCmdUse = "cargoship COMMAND"
	// RootGroupInstallID subcommand for install id
	RootGroupInstallID = "install"
	// RootGroupInstallTitle subcommand for install title
	RootGroupInstallTitle = "Install Commands:"
	// RootGroupPackageID subcommand for package id
	RootGroupPackageID = "package"
	// RootGroupPackageTitle subcommand for package id
	RootGroupPackageTitle = "Package Commands:"
)
