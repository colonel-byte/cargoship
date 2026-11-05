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
	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/spf13/cobra"
)

// newDevCommand groups the commands that serve someone working on a distro definition or on
// cargoship itself, rather than someone operating a fleet. What they have in common is that none
// of them produces an artifact to carry, keep, or sign, which is why they are not at the root. It
// runs nothing itself.
func newDevCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: lang.CmdDevShort,
		Long:  lang.CmdDevLong,
	}

	cmd.AddCommand(newDevApplyCommand())
	cmd.AddCommand(newSha256SumCommand())
	cmd.AddCommand(newCleanCacheCommand())
	cmd.AddCommand(newRedactReceiptCommand())

	return cmd
}
