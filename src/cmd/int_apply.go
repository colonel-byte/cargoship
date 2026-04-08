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

	"github.com/colonel-byte/zarf-distro/src/config/lang"
	"github.com/colonel-byte/zarf-distro/src/pkg/action"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	INSTALL_APPLY_CONFIG = "config"
)

type installApplyOptions struct {
	InstallCommon
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

	cmd.Flags().IntVar(&o.concurrency, "concurrency", v.GetInt(VInstallConcurrency), lang.CmdInstallFlagConcurrency)
	cmd.Flags().StringVar(&o.config, INSTALL_APPLY_CONFIG, "", lang.CmdInstallFlagConfig)

	val, err := cmd.Flags().GetString(ROOT_LOGGING_LEVEL)
	if err != nil {
		val = LOGGING_LEVEL_DEFAULT
	}

	o.logLevel = val

	val, err = cmd.Flags().GetString(ROOT_LOGGING_FORMART)
	if err != nil {
		val = string(logger.FormatConsole)
	}

	o.LogFormat = val

	cmd.MarkFlagRequired(INSTALL_APPLY_CONFIG)

	return cmd
}

func (o *installApplyOptions) run(ctx context.Context, args []string) error {
	l := logger.From(ctx)
	err := initRigLogger(ctx, o.InstallCommon)
	if err != nil {
		l.Warn("failed to configure logger", "err", err)
		return err
	}

	manager, err := initManager(ctx, args[0], o.InstallCommon)
	if err != nil {
		l.Warn("failed to create manager", "err", err)
		return err
	}
	// // deletes the temp directory at the end of the apply phases
	// defer func() {
	// 	l.Debug("removing staging dir", "temp", manager.TempDirectory)
	// 	os.RemoveAll(manager.TempDirectory)
	// }()

	applyOpts := action.ApplyOptions{
		Manager: manager,
	}

	applyAction := action.NewApply(applyOpts)

	if err := applyAction.Run(ctx); err != nil {
		return err
	}

	return nil
}
