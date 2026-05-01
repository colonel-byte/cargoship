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
	"fmt"
	"os"

	"github.com/colonel-byte/cargoship/src/pkg/lint"
	"github.com/fatih/color"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/message"
)

var (
	// OutputWriter provides a default writer to Stdout for user-facing command output
	OutputWriter = os.Stdout
)

// PrintFindings prints the findings in the LintError as a table.
func PrintFindings(ctx context.Context, lintErr *lint.LintError) {
	lintData := [][]string{}
	for _, finding := range lintErr.Findings {
		sevColor := color.FgWhite
		switch finding.Severity {
		case lint.SevErr:
			sevColor = color.FgRed
		case lint.SevWarn:
			sevColor = color.FgYellow
		}

		lintData = append(lintData, []string{
			colorWrap(string(finding.Severity), sevColor),
			colorWrap(finding.YqPath, color.FgCyan),
			finding.ItemizedDescription(),
		})
	}
	// Print table to our OutputWriter
	logger.From(ctx).Info("linting composed package definition", "name", lintErr.PackageName)
	message.TableWithWriter(OutputWriter, []string{"Type", "Path", "Message"}, lintData)
}

func colorWrap(str string, attr color.Attribute) string {
	if !message.ColorEnabled() || str == "" {
		return str
	}
	return fmt.Sprintf("\x1b[%dm%s\x1b[0m", attr, str)
}
