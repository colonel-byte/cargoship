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

package cmd

import (
	"context"
	"os"

	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/internal/clustercfg"
	"github.com/colonel-byte/cargoship/internal/riglogger"
	"github.com/colonel-byte/cargoship/pkg/action"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type installApplyOptions struct {
	InstallCommon
	workerCon        string
	hosts            bool
	firewall         bool
	labelNodes       bool
	allowUnmanaged   bool
	updateKubeConfig bool
	kubeConfigPath   string
	keyFlags
}

func newInstallApplyCommand() *cobra.Command {
	o := installApplyOptions{}
	cmd := &cobra.Command{
		Use:     "apply [Distro Package]",
		Args:    cobra.ExactArgs(1),
		Short:   lang.CmdDistroApplyShort,
		Long:    lang.CmdDistroApplyLong,
		Example: lang.CmdDistroApplyExample,
		GroupID: lang.RootGroupInstallID,
		PreRunE: o.preRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return o.run(ctx, cmd, args)
		},
	}

	cmd.Flags().IntVarP(&o.concurrency, InstallConcurrency, "c", resolvedConfig.DistroOpts.Concurrency, lang.CmdInstallFlagConcurrency)
	cmd.Flags().StringVar(&o.config, InstallConfig, "", lang.CmdInstallFlagConfig)
	cmd.Flags().BoolVar(&o.confirm, InstallConfirm, false, lang.CmdInstallFlagConfirm)
	cmd.Flags().BoolVar(&o.dryRun, InstallDryRun, false, lang.CmdInstallFlagDryRun)
	registerFlagReceiptPath(cmd, &o.receiptPath)
	cmd.Flags().BoolVarP(&o.hosts, InstallUpdateHost, "H", resolvedConfig.DistroOpts.HostUpdate, lang.CmdInstallHostUpdate)
	cmd.Flags().BoolVarP(&o.firewall, InstallUpdateFirewall, "F", resolvedConfig.DistroOpts.FirewallUpdate, lang.CmdInstallFirewallUpdate)
	cmd.Flags().BoolVar(&o.updateKubeConfig, InstallUpdateKubeConfig, resolvedConfig.DistroOpts.UpdateKubeConfig, lang.CmdInstallUpdateKubeConfig)
	cmd.Flags().StringVar(&o.kubeConfigPath, InstallKubeConfigPath, resolvedConfig.DistroOpts.KubeConfig, lang.CmdInstallKubeConfigPath)
	cmd.Flags().BoolVar(&o.labelNodes, InstallLabelNodes, resolvedConfig.DistroOpts.LabelNodes, lang.CmdInstallLabelNodes)
	cmd.Flags().BoolVar(&o.allowUnmanaged, InstallAllowUnmanagedNodes, resolvedConfig.DistroOpts.AllowUnmanagedNodes, lang.CmdInstallAllowUnmanagedNodes)
	cmd.Flags().BoolVar(&o.allowDowngrade, InstallAllowDowngrade, resolvedConfig.DistroOpts.AllowDowngrade, lang.CmdInstallAllowDowngrade)
	cmd.Flags().StringVarP(&o.workerCon, InstallWorkConcurrency, "w", resolvedConfig.DistroOpts.WorkerConcurrency, lang.CmdInstallFlagWorkerConcurrency)
	cmd.Flags().StringVar(&o.vaultPasswordFile, InstallVaultPasswordFile, "", lang.CmdInstallFlagVaultPasswordFile)
	cmd.Flags().StringArrayVar(&o.values, InstallValues, nil, lang.CmdInstallFlagValues)
	addAgeFlags(cmd, &o.keyFlags)

	addVerifyFlags(cmd, &o.packageVerifyFlags)

	markRequired(cmd, InstallConfig)

	addBuildFlags(cmd)
	addTimeoutFlag(cmd)

	return cmd
}

func (o *installApplyOptions) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	l := logger.From(ctx)

	// Opened before anything that can fail, so a run that never reaches its phases still leaves
	// a receipt saying so. runErr is what the receipt records as the outcome, which is why every
	// return below assigns it rather than returning directly.
	var (
		manager      *phase.Manager
		distroLayout *layout.DistroLayout
		runErr       error
	)
	ctx, finishReceipt, err := startReceipt(ctx, cmd, o.InstallCommon)
	if err != nil {
		return err
	}
	defer func() { finishReceipt(manager, args[0], distroLayout, runErr) }()

	d, runErr := preflightInstall(args[0], o.config, o.confirm, o.dryRun)
	if runErr != nil {
		return runErr
	}

	if runErr = riglogger.RigLogger(ctx); runErr != nil {
		return runErr
	}

	manager, distroLayout, runErr = initManager(ctx, args[0], o.InstallCommon)
	if runErr != nil {
		return runErr
	}
	// deletes the temp directory at the end of the apply phases
	defer func() {
		l.Debug("removing staging dir", "temp", manager.TempDirectory)
		if err := os.RemoveAll(manager.TempDirectory); err != nil {
			l.Warn("failed to remove", "folder", manager.TempDirectory)
		}
	}()

	manager.SetTimeout(d)

	// Allowed to come back empty: a configuration holding no encrypted credential needs no key,
	// and demanding one would break every plaintext configuration that works today.
	keyring, runErr := o.resolveKeyring(cmd)
	if runErr != nil {
		return runErr
	}

	// Nothing decrypts these until the engine configuration is written, which is well after every
	// host has been connected to. Check them here, while stopping still costs nothing. It matters
	// more for age than it ever did for vault: an age value records nothing about which recipients
	// it was encrypted to, so this is the only check that catches a configuration encrypted to a
	// key nobody on this machine holds.
	if runErr = clustercfg.VerifyRegistryAuth(manager.Config, keyring); runErr != nil {
		return runErr
	}

	applyOpts := action.ApplyOptions{
		Manager:             manager,
		ModifyHosts:         o.hosts,
		WorkerConcurrent:    o.workerCon,
		ModifyFirewall:      o.firewall,
		LabelNodes:          o.labelNodes,
		AllowUnmanagedNodes: o.allowUnmanaged,
		AllowDowngrade:      o.allowDowngrade,
		UpdateKubeConfig:    o.updateKubeConfig,
		KubeConfigPath:      o.kubeConfigPath,
		Keyring:             keyring,
	}

	apply, runErr := action.NewApply(applyOpts)
	if runErr != nil {
		return runErr
	}

	return apply.Run(ctx)
}
