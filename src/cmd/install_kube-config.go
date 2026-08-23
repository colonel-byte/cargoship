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

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/internal/riglogger"
	"github.com/colonel-byte/cargoship/src/pkg/action"
	"github.com/colonel-byte/cargoship/src/pkg/packager/load"
	"github.com/colonel-byte/cargoship/src/pkg/phase"
	"github.com/colonel-byte/cargoship/src/types"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	// InstallKubeConfig flag
	InstallKubeConfig = "config"
	// InstallKubeConfirm flag
	InstallKubeConfirm = "confirm"
	// InstallKubeDistro flag
	InstallKubeDistro = "distro"
)

type installKubeConfigOptions struct {
	InstallCommon
	distro string
}

func newInstallKubeConfigCommand() *cobra.Command {
	o := installKubeConfigOptions{}
	cmd := &cobra.Command{
		Use:     "kube-config",
		Args:    cobra.ExactArgs(0),
		Short:   lang.CmdDistroKubeConfigShort,
		GroupID: lang.RootGroupInstallID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return o.run(ctx, args)
		},
	}

	cmd.Flags().StringVar(&o.config, InstallKubeConfig, "", lang.CmdInstallFlagConfig)
	cmd.Flags().StringVarP(&o.distro, InstallKubeDistro, "D", resolvedConfig.DistroOpts.Type, lang.CmdInstallFlagKubeConfigDistro)

	val, err := cmd.Flags().GetString(RootLoggingLevel)
	if err != nil {
		val = types.LoggingLevelDefault
	}

	o.logLevel = val

	val, err = cmd.Flags().GetString(RootLoggingFormat)
	if err != nil {
		val = string(logger.FormatConsole)
	}

	o.LogFormat = val

	return cmd
}

func (o *installKubeConfigOptions) run(ctx context.Context, _ []string) error {
	l := logger.From(ctx)

	if err := riglogger.RigLogger(ctx); err != nil {
		l.Warn("failed to configure logger", "err", err)
		return err
	}

	clusterDef, err := load.ClusterDefinition(ctx, o.config, load.ClusterOptions{})
	if err != nil {
		return err
	}

	clusterDef.Spec.Hosts = cluster.ZarfHosts{
		clusterDef.Spec.Hosts.Controllers().First(),
	}

	configOpts := action.KubeConfigOptions{
		Manager: &phase.Manager{
			DistroID:          o.distro,
			Concurrency:       o.concurrency,
			ConcurrentUploads: o.concurrency,
			Config:            &clusterDef,
		},
	}

	return action.NewKubeConfig(configOpts).Run(ctx)
}
