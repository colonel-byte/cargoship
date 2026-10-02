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

	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/internal/clustercfg"
	"github.com/colonel-byte/cargoship/internal/riglogger"
	"github.com/colonel-byte/cargoship/pkg/action"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type installEngineConfigSyncOptions struct {
	InstallCommon
	workerCon        string
	labelNodes       bool
	updateKubeConfig bool
	kubeConfigPath   string
	keyFlags
}

func newInstallEngineConfigSyncCommand() *cobra.Command {
	o := installEngineConfigSyncOptions{}
	cmd := &cobra.Command{
		Use:     "engine-config-sync [Distro Package]",
		Args:    cobra.ExactArgs(1),
		Short:   lang.CmdDistroEngineConfigSyncShort,
		Long:    lang.CmdDistroEngineConfigSyncLong,
		Example: lang.CmdDistroEngineConfigSyncExample,
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
	cmd.Flags().StringVarP(&o.workerCon, InstallWorkConcurrency, "w", resolvedConfig.DistroOpts.WorkerConcurrency, lang.CmdInstallFlagWorkerConcurrency)
	cmd.Flags().BoolVar(&o.updateKubeConfig, InstallUpdateKubeConfig, resolvedConfig.DistroOpts.UpdateKubeConfig, lang.CmdInstallUpdateKubeConfig)
	cmd.Flags().StringVar(&o.kubeConfigPath, InstallKubeConfigPath, resolvedConfig.DistroOpts.KubeConfig, lang.CmdInstallKubeConfigPath)
	cmd.Flags().BoolVar(&o.labelNodes, InstallLabelNodes, resolvedConfig.DistroOpts.LabelNodes, lang.CmdInstallLabelNodes)
	cmd.Flags().StringVar(&o.vaultPasswordFile, InstallVaultPasswordFile, "", lang.CmdInstallFlagVaultPasswordFile)
	cmd.Flags().StringArrayVar(&o.values, InstallValues, nil, lang.CmdInstallFlagValues)
	addAgeFlags(cmd, &o.keyFlags)

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

	markRequired(cmd, InstallConfig)

	addBuildFlags(cmd)
	addTimeoutFlag(cmd)

	return cmd
}

func (o *installEngineConfigSyncOptions) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	d, err := preflightInstall(args[0], o.config, o.confirm, o.dryRun)
	if err != nil {
		return err
	}

	if err := riglogger.RigLogger(ctx); err != nil {
		return err
	}

	manager, err := initManager(ctx, cmd, args[0], o.InstallCommon)
	if err != nil {
		return err
	}

	manager.SetTimeout(d)

	// Allowed to come back empty: a configuration holding no encrypted credential needs no key,
	// and demanding one would break every plaintext configuration that works today.
	keyring, err := o.resolveKeyring(cmd)
	if err != nil {
		return err
	}

	// Nothing decrypts these until the engine configuration is written, which is well after every
	// host has been connected to. Check them here, while stopping still costs nothing.
	if err := clustercfg.VerifyRegistryAuth(manager.Config, keyring); err != nil {
		return err
	}

	engineConfigSyncOpts := action.EngineConfigSyncOptions{
		Manager:          manager,
		WorkerConcurrent: o.workerCon,
		Keyring:          keyring,
		LabelNodes:       o.labelNodes,
		UpdateKubeConfig: o.updateKubeConfig,
		KubeConfigPath:   o.kubeConfigPath,
	}

	return action.NewEngineConfigSync(engineConfigSyncOpts).Run(ctx)
}
