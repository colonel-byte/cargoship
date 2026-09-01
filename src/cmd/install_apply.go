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
	"errors"
	"os"
	"time"

	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/internal/clustercfg"
	"github.com/colonel-byte/cargoship/src/internal/riglogger"
	"github.com/colonel-byte/cargoship/src/pkg/action"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type installApplyOptions struct {
	InstallCommon
	workerCon         string
	hosts             bool
	firewall          bool
	fapolicy          bool
	labelNodes        bool
	updateKubeConfig  bool
	vaultPasswordFile string
}

func newInstallApplyCommand() *cobra.Command {
	o := installApplyOptions{}
	cmd := &cobra.Command{
		Use:     "apply [Distro Package]",
		Args:    cobra.ExactArgs(1),
		Short:   lang.CmdDistroApplyShort,
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
	cmd.Flags().BoolVarP(&o.hosts, InstallUpdateHost, "H", resolvedConfig.DistroOpts.HostUpdate, lang.CmdInstallHostUpdate)
	cmd.Flags().BoolVarP(&o.firewall, InstallUpdateFirewall, "F", resolvedConfig.DistroOpts.FirewallUpdate, lang.CmdInstallFirewallUpdate)
	cmd.Flags().BoolVarP(&o.fapolicy, InstallUpdateFAPolicyD, "f", resolvedConfig.DistroOpts.FAPolicyd, lang.CmdInstallFapolicydUpdate)
	cmd.Flags().BoolVar(&o.updateKubeConfig, InstallUpdateKubeConfig, resolvedConfig.DistroOpts.UpdateKubeConfig, lang.CmdInstallUpdateKubeConfig)
	cmd.Flags().BoolVar(&o.labelNodes, InstallLabelNodes, resolvedConfig.DistroOpts.LabelNodes, lang.CmdInstallLabelNodes)
	cmd.Flags().StringVarP(&o.workerCon, InstallWorkConcurrency, "w", resolvedConfig.DistroOpts.WorkerConcurrency, lang.CmdInstallFlagWorkerConcurrency)
	cmd.Flags().StringVar(&o.vaultPasswordFile, InstallVaultPasswordFile, "", lang.CmdInstallFlagVaultPasswordFile)

	addVerifyFlags(cmd, v, &o.packageVerifyFlags)

	val, err := cmd.Flags().GetString(RootLoggingLevel)
	if err != nil {
		val = loggingLevelDefault
	}

	o.logLevel = val

	val, err = cmd.Flags().GetString(RootLoggingFormat)
	if err != nil {
		val = string(logger.FormatConsole)
	}

	o.LogFormat = val

	cmd.MarkFlagRequired(InstallConfig)

	return cmd
}

func (o *installApplyOptions) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	l := logger.From(ctx)

	if !o.confirm {
		l.Warn("please include the --confirm argument")
		return errors.New("pass confirm argument")
	}

	if err := riglogger.RigLogger(ctx); err != nil {
		l.Warn("failed to configure logger", "err", err)
		return err
	}

	manager, err := initManager(ctx, cmd, args[0], o.InstallCommon)
	if err != nil {
		l.Warn("failed to create manager", "err", err)
		return err
	}
	// deletes the temp directory at the end of the apply phases
	defer func() {
		l.Debug("removing staging dir", "temp", manager.TempDirectory)
		if err := os.RemoveAll(manager.TempDirectory); err != nil {
			l.Warn("failed to remove", "folder", manager.TempDirectory)
		}
	}()

	d, err := time.ParseDuration(Timeout)
	if err != nil {
		l.Warn("failed to parse timeout", "err", err)
		return err
	}

	manager.SetTimout(d)

	vaultPassword, err := clustercfg.ResolveVaultPassword(o.vaultPasswordFile)
	if err != nil {
		l.Warn("failed to resolve vault password", "err", err)
		return err
	}

	applyOpts := action.ApplyOptions{
		Manager:          manager,
		ModifyHosts:      o.hosts,
		WorkerConcurrent: o.workerCon,
		ModifyFirewall:   o.firewall,
		LabelNodes:       o.labelNodes,
		UpdateKubeConfig: o.updateKubeConfig,
		VaultPassword:    vaultPassword,
	}

	return action.NewApply(applyOpts).Run(ctx)
}
