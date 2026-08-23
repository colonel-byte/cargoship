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

// Package flags provides shell completion functions for cargoship CLI flags.
package flags

import (
	"fmt"

	"github.com/colonel-byte/cargoship/src/config"
	"github.com/spf13/cobra"
)

// RegisterOutputFormat provides shell completion suggestions for the output format flag.
func RegisterOutputFormat(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return getOutputFormat(), cobra.ShellCompDirectiveNoFileComp
}

const (
	outputFormatJSONDescription = "output as JSON"
	outputFormatYAMLDescription = "output as YAML"
)

func getOutputFormat() []string {
	return []string{
		fmt.Sprintf("%s\t%s", string(config.OutputFromatJSON), outputFormatJSONDescription),
		fmt.Sprintf("%s\t%s", string(config.OutputFromatYAML), outputFormatYAMLDescription),
	}
}
