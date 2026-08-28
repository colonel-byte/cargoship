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
	"github.com/colonel-byte/cargoship/src/pkg/packager/load"
	"github.com/colonel-byte/cargoship/src/pkg/phase"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	// InstallRegistrySyncConfig flag
	InstallRegistrySyncConfig = "config"
	// InstallRegistrySyncConfirm flag
	InstallRegistrySyncConfirm = "confirm"
	// InstallRegistrySyncDistro flag
	InstallRegistrySyncDistro = "distro"
	// InstallRegistrySyncConcurrency flag
	InstallRegistrySyncConcurrency = "concurrency"
	// InstallRegistrySyncWorkConcurrency flag
	InstallRegistrySyncWorkConcurrency = "work-concurrency"
)

type installRegistrySyncOptions struct {
	InstallCommon
	workerCon         int
	distro            string
	vaultPasswordFile string
}

func newInstallRegistrySyncCommand() *cobra.Command {
	o := installRegistrySyncOptions{}
	cmd := &cobra.Command{
		Use:     "registry-sync",
		Args:    cobra.ExactArgs(0),
		Short:   lang.CmdDistroRegistrySyncShort,
		GroupID: lang.RootGroupInstallID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return o.run(ctx, args)
		},
	}

	cmd.Flags().IntVarP(&o.concurrency, InstallRegistrySyncConcurrency, "c", resolvedConfig.DistroOpts.Concurrency, lang.CmdInstallFlagConcurrency)
	cmd.Flags().StringVar(&o.config, InstallRegistrySyncConfig, "", lang.CmdInstallFlagConfig)
	cmd.Flags().StringVarP(&o.distro, InstallRegistrySyncDistro, "D", resolvedConfig.DistroOpts.Type, lang.CmdInstallFlagRegistrySyncDistro)
	cmd.Flags().BoolVar(&o.confirm, InstallRegistrySyncConfirm, false, lang.CmdInstallFlagConfirm)
	cmd.Flags().IntVarP(&o.workerCon, InstallRegistrySyncWorkConcurrency, "w", resolvedConfig.DistroOpts.WorkerConcurrency, lang.CmdInstallFlagWorkerConcurrency)
	cmd.Flags().StringVar(&o.vaultPasswordFile, InstallVaultPasswordFile, "", lang.CmdInstallFlagVaultPasswordFile)

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

	cmd.MarkFlagRequired(InstallRegistrySyncConfig)

	return cmd
}

func (o *installRegistrySyncOptions) run(ctx context.Context, _ []string) error {
	l := logger.From(ctx)

	if !o.confirm {
		l.Warn("please include the --confirm argument")
		return errors.New("pass confirm argument")
	}

	if err := riglogger.RigLogger(ctx); err != nil {
		l.Warn("failed to configure logger", "err", err)
		return err
	}

	cluster, err := load.ClusterDefinition(ctx, o.config, load.ClusterOptions{})
	if err != nil {
		return err
	}

	manager := &phase.Manager{
		DistroID:          o.distro,
		Concurrency:       o.concurrency,
		ConcurrentUploads: o.concurrency,
		Config:            &cluster,
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

	registrySyncOpts := action.RegistrySyncOptions{
		Manager:          manager,
		WorkerConcurrent: o.workerCon,
		VaultPassword:    vaultPassword,
	}

	return action.NewRegistrySync(registrySyncOpts).Run(ctx)
}
