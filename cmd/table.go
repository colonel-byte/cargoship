// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
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
	"os"

	"github.com/colonel-byte/cargoship/pkg/lint"
	"github.com/fatih/color"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/message"
)

var (
	// OutputWriter is where the human-facing summary tables go -- the lint findings below and
	// the per-host receipt summary in receipt.go. It is stderr, not stdout: both are diagnostics
	// an operator reads, not documents a caller parses, and sending them to stdout meant
	// "cargoship apply > out" captured a box-drawing table instead of nothing.
	OutputWriter = os.Stderr
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
	// Header and table go to the same stream; split across stdout and stderr, "2>/dev/null"
	// left a headerless table and ">/dev/null" a header with no findings.
	logger.From(ctx).Info("linting composed package definition", "name", lintErr.PackageName)
	message.Table(OutputWriter, []string{"Type", "Path", "Message"}, lintData)
}

// colorWrap paints str with attr when color is wanted. It defers the whole decision to
// colorEnabled so the tables agree with the logger, and hands the escape sequences to
// fatih/color rather than spelling them out -- the hand-rolled version it replaces consulted
// only pterm, which never checks whether the stream is a terminal on anything but Windows.
func colorWrap(str string, attr color.Attribute) string {
	if str == "" || !colorEnabled() {
		return str
	}
	c := color.New(attr)
	c.EnableColor()
	return c.Sprint(str)
}
