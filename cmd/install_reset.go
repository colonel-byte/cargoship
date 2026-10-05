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

	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/internal/riglogger"
	"github.com/colonel-byte/cargoship/pkg/action"
	"github.com/colonel-byte/cargoship/pkg/packager/load"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type installResetOptions struct {
	InstallCommon
	workerCon string
	distro    string
}

func newInstallResetCommand() *cobra.Command {
	o := installResetOptions{}
	cmd := &cobra.Command{
		Use:     "reset",
		Args:    cobra.ExactArgs(0),
		Short:   lang.CmdDistroResetShort,
		Long:    lang.CmdDistroResetLong,
		Example: lang.CmdDistroResetExample,
		GroupID: lang.RootGroupInstallID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return o.run(ctx, args)
		},
	}

	cmd.Flags().IntVarP(&o.concurrency, InstallConcurrency, "c", resolvedConfig.DistroOpts.Concurrency, lang.CmdInstallFlagConcurrency)
	cmd.Flags().StringVar(&o.config, InstallConfig, "", lang.CmdInstallFlagConfig)
	cmd.Flags().StringVarP(&o.distro, InstallDistro, "D", resolvedConfig.DistroOpts.Type, lang.CmdInstallFlagResetDistro)
	cmd.Flags().BoolVar(&o.confirm, InstallConfirm, false, lang.CmdInstallFlagConfirm)
	cmd.Flags().BoolVar(&o.dryRun, InstallDryRun, false, lang.CmdInstallFlagDryRun)
	cmd.Flags().StringVarP(&o.workerCon, InstallWorkConcurrency, "w", resolvedConfig.DistroOpts.WorkerConcurrency, lang.CmdInstallFlagWorkerConcurrency)

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

	return cmd
}

func (o *installResetOptions) run(ctx context.Context, _ []string) error {
	if err := preflightReset(o.config, o.confirm, o.dryRun); err != nil {
		return err
	}

	if err := riglogger.RigLogger(ctx); err != nil {
		return err
	}

	cluster, err := load.ClusterDefinition(ctx, o.config, load.ClusterOptions{})
	if err != nil {
		return err
	}

	resetOpts := action.ResetOptions{
		Manager: &phase.Manager{
			DistroID:          o.distro,
			Concurrency:       o.concurrency,
			ConcurrentUploads: o.concurrency,
			Config:            &cluster,
			// Reset does not load a package, so it builds its Manager here rather than through
			// initManager, and this is the only place the flag reaches it.
			DryRun: o.dryRun,
		},
		WorkerConcurrent: o.workerCon,
		NoWait:           true,
		NoDrain:          true,
	}

	return action.NewReset(resetOpts).Run(ctx)
}
