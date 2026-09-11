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

package cmd

import (
	"context"
	"fmt"

	"github.com/colonel-byte/cargoship/src/cmd/flags"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	zlang "github.com/zarf-dev/zarf/src/config/lang"
)

// registerFlagOCIConcurrency adds the OCI concurrency flag to cmd, with its default sourced
// from config and shell completion suggestions provided by flags.RegisterOCIConcurrency.
func registerFlagOCIConcurrency(cmd *cobra.Command, con *int) error {
	cmd.Flags().IntVar(con, PackageOCIConcurrency, resolvedConfig.DistroOpts.OCIConcurrency, lang.CmdPackageFlagConcurrency)
	return cmd.RegisterFlagCompletionFunc(PackageOCIConcurrency, flags.RegisterOCIConcurrency)
}

// registerFlagOutputFormat adds the output format flag to cmd, backed by format, with shell
// completion suggestions provided by flags.RegisterOutputFormat.
func registerFlagOutputFormat(cmd *cobra.Command, format pflag.Value) error {
	cmd.Flags().VarP(format, MiscOutput, "o", lang.CmdVersionOutputFromat)
	return cmd.RegisterFlagCompletionFunc(MiscOutput, flags.RegisterOutputFormat)
}

// buildFlagGroupTitle is the usage section title for the flags that decide where a command
// builds, caches, and stages package content.
const buildFlagGroupTitle = "Build Flags"

// registryFlagGroupTitle is the usage section title for the flags that change how cargoship
// talks to a container registry.
const registryFlagGroupTitle = "Registry Flags"

// newTempDirFlagSet returns the staging-directory flag. It is split out from newBuildFlagSet
// so sha256sum can take it alone -- that command stages a download but has no package cache
// and no architecture to select.
func newTempDirFlagSet(ctx context.Context) *pflag.FlagSet {
	fs := pflag.NewFlagSet("tmpdir", pflag.ContinueOnError)
	fs.StringVar(&config.CommonOptions.TempDirectory, "tmpdir", parsePath(ctx, resolvedConfig.TempDirectory), zlang.RootCmdFlagTempDir)
	annotateFlagGroup(fs, buildFlagGroupTitle)
	return fs
}

// newBuildFlagSet returns the flags shared by every command that creates or loads a package
// layout: the target architecture, the package cache, and the staging directory.
func newBuildFlagSet(ctx context.Context) *pflag.FlagSet {
	fs := pflag.NewFlagSet("build", pflag.ContinueOnError)

	fs.StringVarP(&config.CLIArch, RootArchitecture, "a", resolvedConfig.Architecture, zlang.RootCmdFlagArch)
	fs.StringVar(&config.CommonOptions.CachePath, RootZarfCache, parsePath(ctx, resolvedConfig.CachePath), zlang.RootCmdFlagCachePath)
	fs.AddFlagSet(newTempDirFlagSet(ctx))

	annotateFlagGroup(fs, buildFlagGroupTitle)
	return fs
}

// newRegistryFlagSet returns the transport flags read by defaultRemoteOptions. Only the
// commands that reach a registry register them.
func newRegistryFlagSet() *pflag.FlagSet {
	fs := pflag.NewFlagSet("registry", pflag.ContinueOnError)

	fs.BoolVar(&plainHTTP, "plain-http", v.GetBool(RootPlainHTTP), zlang.RootCmdFlagPlainHTTP)
	fs.BoolVar(&insecureSkipTLSVerify, "insecure-skip-tls-verify", v.GetBool(RootInsecureSkipTLSVerify), zlang.RootCmdFlagInsecureSkipTLSVerify)

	annotateFlagGroup(fs, registryFlagGroupTitle)
	return fs
}

// addTempDirFlags registers the staging-directory flag on cmd.
func addTempDirFlags(cmd *cobra.Command) {
	cmd.Flags().AddFlagSet(newTempDirFlagSet(cmd.Context()))
}

// addBuildFlags registers the package build flags on cmd, along with shell completion for
// --architecture. Completion is registered here rather than in newBuildFlagSet because
// RegisterFlagCompletionFunc needs the flag to already be present on the command.
func addBuildFlags(cmd *cobra.Command) {
	cmd.Flags().AddFlagSet(newBuildFlagSet(cmd.Context()))
	if err := cmd.RegisterFlagCompletionFunc(RootArchitecture, flags.RegisterArchitectureFormat); err != nil {
		fmt.Printf("failed to register %s flag completion: %v", RootArchitecture, err)
	}
}

// addRegistryFlags registers the registry transport flags on cmd.
func addRegistryFlags(cmd *cobra.Command) {
	cmd.Flags().AddFlagSet(newRegistryFlagSet())
}

// addTimeoutFlag registers the timeout flag on cmd. Only the install commands that wait on a
// cluster read Timeout.
func addTimeoutFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&Timeout, RootTimeout, v.GetString(RootTimeout), lang.CmdInstallFlagTimeout)
}
