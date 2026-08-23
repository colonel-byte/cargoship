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

	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/internal/riglogger"
	"github.com/colonel-byte/cargoship/src/pkg/action"
	"github.com/colonel-byte/cargoship/src/pkg/packager/load"
	"github.com/colonel-byte/cargoship/src/pkg/phase"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	// InstallResetConfig flag
	InstallResetConfig = "config"
	// InstallResetConfirm flag
	InstallResetConfirm = "confirm"
	// InstallResetDistro flag
	InstallResetDistro = "distro"
	// InstallResetConcurrency flag
	InstallResetConcurrency = "concurrency"
	// InstallResetWorkConcurrency flag
	InstallResetWorkConcurrency = "work-concurrency"
	// InstallResetUpdateHost flag
	InstallResetUpdateHost = "update-hosts"
	// InstallResetUpdateFirewall flag
	InstallResetUpdateFirewall = "update-firewall"
	// InstallResetUpdateFAPolicyD flag
	InstallResetUpdateFAPolicyD = "update-fapolicyd"
)

type installResetOptions struct {
	InstallCommon
	workerCon int
	hosts     bool
	firewall  bool
	fapolicy  bool
	distro    string
}

func newInstallResetCommand() *cobra.Command {
	o := installResetOptions{}
	cmd := &cobra.Command{
		Use:     "reset",
		Args:    cobra.ExactArgs(0),
		Short:   lang.CmdDistroResetShort,
		GroupID: lang.RootGroupInstallID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return o.run(ctx, args)
		},
	}

	cmd.Flags().IntVarP(&o.concurrency, InstallResetConcurrency, "c", resolvedConfig.DistroOpts.Concurrency, lang.CmdInstallFlagConcurrency)
	cmd.Flags().StringVar(&o.config, InstallResetConfig, "", lang.CmdInstallFlagConfig)
	cmd.Flags().StringVarP(&o.distro, InstallResetDistro, "D", resolvedConfig.DistroOpts.Type, lang.CmdInstallFlagResetDistro)
	cmd.Flags().BoolVar(&o.confirm, InstallResetConfirm, false, lang.CmdInstallFlagConfirm)
	cmd.Flags().BoolVarP(&o.hosts, InstallResetUpdateHost, "H", resolvedConfig.DistroOpts.HostUpdate, lang.CmdInstallHostUpdate)
	cmd.Flags().BoolVarP(&o.firewall, InstallResetUpdateFirewall, "F", resolvedConfig.DistroOpts.FirewallUpdate, lang.CmdInstallFirewallUpdate)
	cmd.Flags().BoolVarP(&o.fapolicy, InstallResetUpdateFAPolicyD, "f", resolvedConfig.DistroOpts.FAPolicyd, lang.CmdInstallFapolicydUpdate)
	cmd.Flags().IntVarP(&o.workerCon, InstallResetWorkConcurrency, "w", resolvedConfig.DistroOpts.WorkerConcurrency, lang.CmdInstallFlagWorkerConcurrency)

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

	cmd.MarkFlagRequired(InstallResetConfig)

	return cmd
}

func (o *installResetOptions) run(ctx context.Context, _ []string) error {
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

	resetOpts := action.ResetOptions{
		Manager: &phase.Manager{
			DistroID:          o.distro,
			Concurrency:       o.concurrency,
			ConcurrentUploads: o.concurrency,
			Config:            &cluster,
		},
		WorkerConcurrent: o.workerCon,
		NoWait:           true,
		NoDrain:          true,
	}

	return action.NewReset(resetOpts).Run(ctx)
}
