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

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/internal/riglogger"
	"github.com/colonel-byte/cargoship/pkg/action"
	"github.com/colonel-byte/cargoship/pkg/packager/load"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type installKubeConfigOptions struct {
	InstallCommon
	distro         string
	kubeConfigPath string
}

func newInstallKubeConfigCommand() *cobra.Command {
	o := installKubeConfigOptions{}
	cmd := &cobra.Command{
		Use:     "kube-config",
		Args:    cobra.ExactArgs(0),
		Short:   lang.CmdDistroKubeConfigShort,
		Long:    lang.CmdDistroKubeConfigLong,
		Example: lang.CmdDistroKubeConfigExample,
		GroupID: lang.RootGroupInstallID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return o.run(ctx, args)
		},
	}

	cmd.Flags().StringVar(&o.config, InstallConfig, "", lang.CmdInstallFlagConfig)
	cmd.Flags().StringVarP(&o.distro, InstallDistro, "D", resolvedConfig.DistroOpts.Type, lang.CmdInstallFlagKubeConfigDistro)
	cmd.Flags().StringVar(&o.kubeConfigPath, InstallKubeConfigPath, resolvedConfig.DistroOpts.KubeConfig, lang.CmdInstallKubeConfigPath)

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

	return cmd
}

func (o *installKubeConfigOptions) run(ctx context.Context, _ []string) error {
	// kube-config changes no host, so there is no --confirm gate here -- only the check that
	// the configuration it was pointed at is there to be read.
	if err := checkClusterConfig(o.config); err != nil {
		return err
	}

	if err := riglogger.RigLogger(ctx); err != nil {
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
		KubeConfigPath: o.kubeConfigPath,
	}

	kubeConfig, err := action.NewKubeConfig(configOpts)
	if err != nil {
		return err
	}
	return kubeConfig.Run(ctx)
}
