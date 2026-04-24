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
	"os"
	"time"

	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/internal/riglogger"
	"github.com/colonel-byte/cargoship/src/pkg/action"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	InstallApplyConfig          = "config"
	InstallApplyConfirm         = "confirm"
	InstallApplyConcurrency     = "concurrency"
	InstallApplyWorkConcurrency = "work-concurrency"
	InstallApplyUpdateHost      = "update-hosts"
	InstallApplyUpdateFirewall  = "update-firewall"
	InstallApplyUpdateFAPolicyD = "update-fapolicyd"
)

type installApplyOptions struct {
	InstallCommon
	workerCon int
	hosts     bool
	firewall  bool
	fapolicy  bool
}

func newInstallApplyCommand() *cobra.Command {
	o := installApplyOptions{}
	cmd := &cobra.Command{
		Use:     "apply [Distro Package]",
		Args:    cobra.ExactArgs(1),
		Short:   lang.CmdDistroCreateShort,
		GroupID: lang.RootGroupInstallID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return o.run(ctx, args)
		},
	}

	cmd.Flags().IntVarP(&o.concurrency, InstallApplyConcurrency, "c", v.GetInt(VInstallConcurrency), lang.CmdInstallFlagConcurrency)
	cmd.Flags().StringVar(&o.config, InstallApplyConfig, "", lang.CmdInstallFlagConfig)
	cmd.Flags().BoolVar(&o.confirm, InstallApplyConfirm, false, lang.CmdInstallFlagConfirm)
	cmd.Flags().BoolVarP(&o.hosts, InstallApplyUpdateHost, "H", v.GetBool(VInstallUpdateHost), lang.CmdInstallHostUpdate)
	cmd.Flags().BoolVarP(&o.firewall, InstallApplyUpdateFirewall, "F", v.GetBool(VInstallUpdateFirewall), lang.CmdInstallFirewallUpdate)
	cmd.Flags().BoolVarP(&o.fapolicy, InstallApplyUpdateFAPolicyD, "f", v.GetBool(VInstallUpdateFirewall), lang.CmdInstallFapolicydUpdate)
	cmd.Flags().IntVarP(&o.workerCon, InstallApplyWorkConcurrency, "w", v.GetInt(VInstallWorkerConcurrency), lang.CmdInstallFlagWorkerConcurrency)

	val, err := cmd.Flags().GetString(ROOT_LOGGING_LEVEL)
	if err != nil {
		val = LOGGING_LEVEL_DEFAULT
	}

	o.logLevel = val

	val, err = cmd.Flags().GetString(RootLoggingFormat)
	if err != nil {
		val = string(logger.FormatConsole)
	}

	o.LogFormat = val

	cmd.MarkFlagRequired(InstallApplyConfig)

	return cmd
}

func (o *installApplyOptions) run(ctx context.Context, args []string) error {
	l := logger.From(ctx)

	if !o.confirm {
		l.Warn("please include the --confirm argument")
		return errors.New("pass confirm argument")
	}

	err := riglogger.RigLogger(ctx)
	if err != nil {
		l.Warn("failed to configure logger", "err", err)
		return err
	}

	manager, err := initManager(ctx, args[0], o.InstallCommon)
	if err != nil {
		l.Warn("failed to create manager", "err", err)
		return err
	}
	// deletes the temp directory at the end of the apply phases
	defer func() {
		l.Debug("removing staging dir", "temp", manager.TempDirectory)
		os.RemoveAll(manager.TempDirectory)
	}()

	d, err := time.ParseDuration(Timeout)
	if err != nil {
		l.Warn("failed to parse timeout", "err", err)
		return err
	}

	manager.SetTimout(d)

	applyOpts := action.ApplyOptions{
		Manager:          manager,
		ModifyHosts:      o.hosts,
		WorkerConcurrent: o.workerCon,
		ModifyFirewall:   o.firewall,
	}

	if err := action.NewApply(applyOpts).Run(ctx); err != nil {
		return err
	}

	return nil
}
