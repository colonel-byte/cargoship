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
	"fmt"

	"github.com/colonel-byte/mare/src/config"
	"github.com/colonel-byte/mare/src/config/lang"
	"github.com/spf13/cobra"
)

type versionOptions struct{}

func newVersionCommand() *cobra.Command {
	o := versionOptions{}

	cmd := &cobra.Command{
		Use:               "version",
		Aliases:           []string{"v"},
		Short:             lang.CmdVersionShort,
		Long:              lang.CmdVersionLong,
		RunE:              o.run,
		PersistentPreRunE: o.perprerun,
	}

	return cmd
}

func (o *versionOptions) perprerun(_ *cobra.Command, _ []string) error {
	return nil
}

func (o *versionOptions) run(_ *cobra.Command, _ []string) error {
	fmt.Println(config.CLIVersion)
	return nil
}
