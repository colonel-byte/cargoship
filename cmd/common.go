// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
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

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/distro"
	"github.com/colonel-byte/cargoship/pkg/helmvalues"
	"github.com/colonel-byte/cargoship/pkg/packager/load"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/colonel-byte/cargoship/pkg/utils"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/colonel-byte/cargoship/types/distrocfg/registry"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/types"
)

// InstallCommon common args
type InstallCommon struct {
	config      string
	concurrency int
	confirm     bool
	// dryRun reaches phase.Manager.DryRun. Only apply, reset, and engine-config-sync register
	// the flag; the other commands embedding InstallCommon carry the field unset, which is the
	// same as off.
	dryRun    bool
	logLevel  string
	LogFormat string
	// values holds the --values files. They are merged after the cluster config's own
	// spec.config.values, so the command line wins over the file. Commands that do not
	// load a package never register the flag and carry the field empty.
	values []string
	// packageVerifyFlags carries the signature verification flags for the install
	// commands that load a package through initManager. Commands that do not load a
	// package (reset, kube-config) embed InstallCommon but never register these flags.
	packageVerifyFlags
}

var plainHTTP bool
var insecureSkipTLSVerify bool
var isCleanPathRegex = regexp.MustCompile(`^[a-zA-Z0-9\_\-\/\.\~\\:]+$`)

// Distro returns the distro config for a given string
func Distro(s string) (distrocfg.Distro, error) {
	ds, err := registry.GetDistroModuleBuilder(s)
	if err != nil {
		return nil, err
	}
	d := ds().(distrocfg.Distro) //nolint:errcheck

	return d, nil
}

func defaultRemoteOptions() types.RemoteOptions {
	return types.RemoteOptions{
		PlainHTTP:             plainHTTP,
		InsecureSkipTLSVerify: insecureSkipTLSVerify,
	}
}

func setBaseDirectory(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "."
}

func getCachePath(ctx context.Context) (string, error) {
	if !isCleanPathRegex.MatchString(config.CommonOptions.CachePath) {
		logger.From(ctx).Warn("invalid characters in cargo-ship cache path, using default", "cfg", config.DefaultCachePath, "default", config.DefaultCachePath)
		config.CommonOptions.CachePath = config.DefaultCachePath
	}
	return config.GetAbsCachePath()
}

func initManager(ctx context.Context, cmd *cobra.Command, distroPath string, opt InstallCommon) (*phase.Manager, error) {
	path, err := filepath.Abs(opt.config)
	if err != nil {
		return nil, err
	}

	opt.config = path

	cluster, err := load.ClusterDefinition(ctx, opt.config, load.ClusterOptions{})
	if err != nil {
		return nil, err
	}

	logger.From(ctx).Info("using cluster file", "location", opt.config)

	cachePath, err := getCachePath(ctx)
	if err != nil {
		return nil, err
	}

	loadOpts := distro.LoadOptions{
		CachePath:            cachePath,
		Architecture:         config.CLIArch,
		Output:               config.CommonOptions.TempDirectory,
		VerificationStrategy: opt.verify.toStrategy(),
		VerifyBlobOptions:    opt.buildVerifyBlobOptions(cmd, v),
	}

	distroLayout, err := distro.Load(ctx, distroPath, loadOpts)
	if err != nil {
		return nil, err
	}

	logger.From(ctx).Debug("distro information", "temp", distroLayout.DirPath(), "build", distroLayout.Distro.Build.Timestamp)

	// The cluster file overrides the values the package was built with, --values
	// overrides both, and the result is checked against the package's schema here,
	// before any phase runs.
	overrides := []map[string]any{cluster.Spec.Config.Values}
	if len(opt.values) > 0 {
		fromFlags, err := helmvalues.LoadFiles(ctx, "", "", opt.values)
		if err != nil {
			return nil, fmt.Errorf("unable to read the values files given with --%s: %w", InstallValues, err)
		}
		overrides = append(overrides, fromFlags)
	}

	values, err := distroLayout.Values(ctx, overrides...)
	if err != nil {
		return nil, err
	}
	// Both of these resolve the package against the values before any phase runs: the
	// first renders and maps the engine configuration, the second renders the contents of
	// the files the package marked as templates, in place in the extracted package.
	if err := distroLayout.ApplyValues(values); err != nil {
		return nil, err
	}
	if err := distroLayout.RenderFiles(ctx, values); err != nil {
		return nil, err
	}

	return &phase.Manager{
		Config:            &cluster,
		Distro:            &distroLayout.Distro,
		Values:            values,
		DistroID:          distroLayout.Distro.Spec.Type,
		TempDirectory:     distroLayout.DirPath(),
		Concurrency:       opt.concurrency,
		ConcurrentUploads: opt.concurrency,
		DryRun:            opt.dryRun,
	}, nil
}

// requireConfirm enforces the --confirm gate for a command that changes the hosts it is pointed at.
//
// A dry run changes nothing, so there is nothing to confirm. Requiring --confirm to ask what would
// happen is what would push someone into running the real thing to find out.
func requireConfirm(confirm, dryRun bool) error {
	if confirm || dryRun {
		return nil
	}
	return fmt.Errorf("this changes every host in the cluster configuration; pass --%s to proceed, or --%s to see what it would do", InstallConfirm, InstallDryRun)
}

// checkClusterConfig reports a --config that names a file nothing can be read from, without
// parsing it. The parse happens later, in initManager or load.ClusterDefinition, which report a
// malformed document far better than a stat can.
func checkClusterConfig(configPath string) error {
	if configPath == "" {
		return fmt.Errorf("no cluster configuration given; pass --%s", InstallConfig)
	}
	path, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("unable to resolve --%s %q: %w", InstallConfig, configPath, err)
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("unable to read the cluster configuration: %w", err)
	}
	return nil
}

// checkPackageSource reports a package source that cannot be fetched from. A local tarball is
// stat'ed; a remote reference is only checked for being a shape cargoship recognises, since
// reaching out to a registry is neither free nor something an air-gapped host can do twice.
func checkPackageSource(source string) error {
	if source == "" {
		return errors.New("no package given")
	}
	srcType, err := utils.IdentifySource(source)
	if err != nil {
		return err
	}
	switch srcType {
	case "tarball", "split":
		if _, err := os.Stat(source); err != nil {
			return fmt.Errorf("unable to read the package: %w", err)
		}
	}
	return nil
}

// preflightInstall validates what an install command was given and then enforces the --confirm
// gate, for the commands that take a package, a cluster configuration, and --timeout.
//
// The order is the point. The gate used to be the first thing each of these commands did, so a run
// naming a package and a configuration that were both absent reported only the missing --confirm.
// Supplying it reported the configuration, and fixing that reported the package: three runs to
// learn three things that were all knowable before the first one started. Everything checked here
// costs a stat and a string parse, so the input mistakes come out together and ahead of the gate --
// and the gate is still cleared before the package is extracted or any host is connected to.
//
// The parsed --timeout comes back with it, so no caller parses it a second time.
func preflightInstall(packageSource, configPath string, confirm, dryRun bool) (time.Duration, error) {
	if err := checkClusterConfig(configPath); err != nil {
		return 0, err
	}
	if err := checkPackageSource(packageSource); err != nil {
		return 0, err
	}
	d, err := time.ParseDuration(Timeout)
	if err != nil {
		return 0, fmt.Errorf("unable to parse --%s %q: %w", RootTimeout, Timeout, err)
	}
	if err := requireConfirm(confirm, dryRun); err != nil {
		return 0, err
	}
	return d, nil
}

// preflightReset is preflightInstall for reset, which loads no package and registers no --timeout.
func preflightReset(configPath string, confirm, dryRun bool) error {
	if err := checkClusterConfig(configPath); err != nil {
		return err
	}
	return requireConfirm(confirm, dryRun)
}

// markRequired marks a flag as required, reporting a failure the way NewCargoshipCommand reports a
// failed completion registration.
//
// MarkFlagRequired only fails when the named flag was never registered, which is a mistake in this
// package rather than anything an operator did. The command builders return no error, so there is
// nowhere to hand it: stderr is where it goes, so that a flag silently not being required shows up
// while it is being written instead of in a run that skipped a check it was meant to make.
func markRequired(cmd *cobra.Command, name string) {
	if err := cmd.MarkFlagRequired(name); err != nil {
		fmt.Fprintf(os.Stderr, "failed to mark the %s flag required: %v\n", name, err)
	}
}
