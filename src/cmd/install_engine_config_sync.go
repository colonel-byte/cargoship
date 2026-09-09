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
	"errors"
	"time"

	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/internal/clustercfg"
	"github.com/colonel-byte/cargoship/src/internal/riglogger"
	"github.com/colonel-byte/cargoship/src/pkg/action"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	// InstallEngineConfigSyncConfig flag
	InstallEngineConfigSyncConfig = "config"
	// InstallEngineConfigSyncConfirm flag
	InstallEngineConfigSyncConfirm = "confirm"
	// InstallEngineConfigSyncConcurrency flag
	InstallEngineConfigSyncConcurrency = "concurrency"
	// InstallEngineConfigSyncWorkConcurrency flag
	InstallEngineConfigSyncWorkConcurrency = "work-concurrency"
)

type installEngineConfigSyncOptions struct {
	InstallCommon
	workerCon         string
	labelNodes        bool
	updateKubeConfig  bool
	vaultPasswordFile string
}

func newInstallEngineConfigSyncCommand() *cobra.Command {
	o := installEngineConfigSyncOptions{}
	cmd := &cobra.Command{
		Use:     "engine-config-sync [Distro Package]",
		Args:    cobra.ExactArgs(1),
		Short:   lang.CmdDistroEngineConfigSyncShort,
		Example: lang.CmdDistroEngineConfigSyncExample,
		GroupID: lang.RootGroupInstallID,
		PreRunE: o.preRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return o.run(ctx, cmd, args)
		},
	}

	cmd.Flags().IntVarP(&o.concurrency, InstallEngineConfigSyncConcurrency, "c", resolvedConfig.DistroOpts.Concurrency, lang.CmdInstallFlagConcurrency)
	cmd.Flags().StringVar(&o.config, InstallEngineConfigSyncConfig, "", lang.CmdInstallFlagConfig)
	cmd.Flags().BoolVar(&o.confirm, InstallEngineConfigSyncConfirm, false, lang.CmdInstallFlagConfirm)
	cmd.Flags().StringVarP(&o.workerCon, InstallEngineConfigSyncWorkConcurrency, "w", resolvedConfig.DistroOpts.WorkerConcurrency, lang.CmdInstallFlagWorkerConcurrency)
	cmd.Flags().BoolVar(&o.updateKubeConfig, InstallUpdateKubeConfig, resolvedConfig.DistroOpts.UpdateKubeConfig, lang.CmdInstallUpdateKubeConfig)
	cmd.Flags().BoolVar(&o.labelNodes, InstallLabelNodes, resolvedConfig.DistroOpts.LabelNodes, lang.CmdInstallLabelNodes)
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

	cmd.MarkFlagRequired(InstallEngineConfigSyncConfig)

	return cmd
}

func (o *installEngineConfigSyncOptions) run(ctx context.Context, cmd *cobra.Command, args []string) error {
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

	engineConfigSyncOpts := action.EngineConfigSyncOptions{
		Manager:          manager,
		WorkerConcurrent: o.workerCon,
		VaultPassword:    vaultPassword,
		LabelNodes:       o.labelNodes,
		UpdateKubeConfig: o.updateKubeConfig,
	}

	return action.NewEngineConfigSync(engineConfigSyncOpts).Run(ctx)
}
