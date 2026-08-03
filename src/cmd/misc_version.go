// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
//
// Modifications Copyright 2026 colonel-byte.
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
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/config/lang"
	goyaml "github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
)

type versionOptions struct {
	outputFormat outputFormat
}

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

	cmd.Flags().VarP(&o.outputFormat, "output", "o", "output format (yaml|json)")

	return cmd
}

func (o *versionOptions) perprerun(_ *cobra.Command, _ []string) error {
	return nil
}

func (o *versionOptions) run(_ *cobra.Command, _ []string) error {
	if o.outputFormat == "" {
		fmt.Println(config.CLIVersion)
		return nil
	}

	buildMap := map[string]string{}
	buildMap["version"] = config.CLIVersion
	buildMap["commit"] = config.CLICommit
	buildMap["platform"] = runtime.GOOS + "/" + runtime.GOARCH
	buildMap["go"] = runtime.Version()

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return errors.New("failed to get build info")
	}
	depMap := map[string]string{}
	for _, dep := range buildInfo.Deps {
		if dep.Replace != nil {
			depMap[dep.Path] = fmt.Sprintf("%s -> %s %s", dep.Version, dep.Replace.Path, dep.Replace.Version)
		} else {
			depMap[dep.Path] = dep.Version
		}
	}
	output := make(map[string]any)
	output["build"] = buildMap
	output["dependencies"] = depMap

	switch o.outputFormat {
	case "yaml":
		b, err := goyaml.Marshal(output)
		if err != nil {
			return fmt.Errorf("could not marshal yaml output: %w", err)
		}
		fmt.Println(string(b))
	case "json":
		b, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("could not marshal json output: %w", err)
		}
		fmt.Println(string(b))
	default:
		return fmt.Errorf("unsupported output format: %s", o.outputFormat)
	}
	return nil
}
