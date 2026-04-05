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
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type installApplyOptions struct {
	concurrency int
	confirm     bool
}

func newInstallApplyCommand() *cobra.Command {
	o := installApplyOptions{}
	cmd := &cobra.Command{
		Use:     "apply",
		Args:    cobra.MaximumNArgs(1),
		Short:   lang.CmdDistroCreateShort,
		GroupID: lang.RootGroupInstallID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return o.run(ctx, args)
		},
	}

	cmd.Flags().IntVar(&o.concurrency, "concurrency", v.GetInt(VInstallConcurrency), lang.CmdInstallFlagConcurrency)

	return cmd
}

func (o *installApplyOptions) run(ctx context.Context, _ []string) error {
	l := logger.From(ctx)

	l.Info("test", "concurrency", o.concurrency)

	return nil
}
