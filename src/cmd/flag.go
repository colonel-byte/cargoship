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
	"github.com/colonel-byte/cargoship/src/cmd/flags"
	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// registerFlagOCIConcurrency adds the OCI concurrency flag to cmd, with its default sourced
// from config and shell completion suggestions provided by flags.RegisterOCIConcurrency.
func registerFlagOCIConcurrency(cmd *cobra.Command, con *int) error {
	cmd.Flags().IntVar(con, PackageOCIConcurrency, v.GetInt(types.DistroOCIConcurrency), lang.CmdPackageFlagConcurrency)
	return cmd.RegisterFlagCompletionFunc(PackageOCIConcurrency, flags.RegisterOCIConcurrency)
}

// registerFlagOutputFormat adds the output format flag to cmd, backed by format, with shell
// completion suggestions provided by flags.RegisterOutputFormat.
func registerFlagOutputFormat(cmd *cobra.Command, format pflag.Value) error {
	cmd.Flags().VarP(format, MiscOutput, "o", lang.CmdVersionOutputFromat)
	return cmd.RegisterFlagCompletionFunc(MiscOutput, flags.RegisterOutputFormat)
}
