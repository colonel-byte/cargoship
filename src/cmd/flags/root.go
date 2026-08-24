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

package flags

import (
	"fmt"

	"github.com/colonel-byte/cargoship/src/config"
	"github.com/spf13/cobra"
)

// RegisterLogLevel provides shell completion suggestions for the log-level flag.
func RegisterLogLevel(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return getLogLevel(), cobra.ShellCompDirectiveNoFileComp
}

const (
	logLevelTraceDescription = "most verbose: every step, including internals"
	logLevelDebugDescription = "verbose: detailed diagnostic information"
	logLevelInfoDescription  = "default: high-level progress messages"
	logLevelWarnDescription  = "quiet: only warnings and errors"
	logLevelErrorDescription = "quietest: only errors"
)

func getLogLevel() []string {
	return []string{
		fmt.Sprintf("%s\t%s", config.RootFlagLogLevelTrace, logLevelTraceDescription),
		fmt.Sprintf("%s\t%s", config.RootFlagLogLevelDebug, logLevelDebugDescription),
		fmt.Sprintf("%s\t%s", config.RootFlagLogLevelInfo, logLevelInfoDescription),
		fmt.Sprintf("%s\t%s", config.RootFlagLogLevelWarn, logLevelWarnDescription),
		fmt.Sprintf("%s\t%s", config.RootFlagLogLevelError, logLevelErrorDescription),
	}
}

// RegisterLogFormat provides shell completion suggestions for the log-format flag.
func RegisterLogFormat(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return getLogFormat(), cobra.ShellCompDirectiveNoFileComp
}

const (
	logFormatConsoleDescription = "default: colorful, human-readable console output"
	logFormatJSONDescription    = "structured JSON, one object per line"
	logFormatDevDescription     = "verbose, pretty-printed output for local development"
)

func getLogFormat() []string {
	return []string{
		fmt.Sprintf("%s\t%s", config.RootFlagLogFormatConsole, logFormatConsoleDescription),
		fmt.Sprintf("%s\t%s", config.RootFlagLogFormatJSON, logFormatJSONDescription),
		fmt.Sprintf("%s\t%s", config.RootFlagLogFormatDev, logFormatDevDescription),
	}
}

// osArchAMD64Desc and osArchARM64Desc are the shell completion descriptions
// shown alongside each supported --architecture value.
const (
	osArchAMD64Desc = "the x86-64, 64-bit AMD, architecture"
	osArchARM64Desc = "the 64-bit ARM architecture"
	osArchRISCVDesc = "the 64-bit RISC-V architecture"
)

// getRootArchitecture returns the valid --architecture
// values as "value\tdescription" pairs, ready to return from a cobra
// RegisterFlagCompletionFunc callback for shell tab completion.
func getRootArchitecture() []string {
	return []string{
		fmt.Sprintf("%s\t%s", string(config.OSArchAMD64), osArchAMD64Desc),
		fmt.Sprintf("%s\t%s", string(config.OSArchARM64), osArchARM64Desc),
		fmt.Sprintf("%s\t%s", string(config.OSArchRISCV), osArchRISCVDesc),
	}
}

// RegisterArchitectureFormat provides shell completion suggestions for the architecture flag.
func RegisterArchitectureFormat(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return getRootArchitecture(), cobra.ShellCompDirectiveNoFileComp
}
