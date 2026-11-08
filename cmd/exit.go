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
	"errors"
	"fmt"

	"github.com/colonel-byte/cargoship/pkg/phase"
	pkgschema "github.com/colonel-byte/cargoship/pkg/schema"
	"github.com/spf13/cobra"
)

// Exit codes cargoship promises. A wrapper script on an air-gapped management node decides whether
// to retry, re-stage, or stop from these, so each one is a compatibility promise: renumbering one
// breaks every caller that reads it. Add a code rather than repurposing one, and document it in
// README.md and AGENTS.md at the same time.
const (
	// ExitOK is a run that did what it was asked.
	ExitOK = 0
	// ExitFailure is anything not classified below. A retry is unlikely to help on its own.
	ExitFailure = 1
	// ExitUsage is a malformed invocation: an unknown flag or command, the wrong number of
	// arguments, a missing required flag. The fix is to the command line, so retrying it is
	// pointless.
	ExitUsage = 2
	// ExitInvalidInput is a file cargoship was pointed at that does not hold what it must: a
	// document that fails its schema, or a config file with a key the schema does not define.
	// The fix is to the file.
	ExitInvalidInput = 3
	// ExitConfirmRequired is a host-changing command run without --confirm. Nothing happened,
	// and the same command with --confirm would proceed.
	ExitConfirmRequired = 4
	// ExitHostUnreachable is a fleet cargoship could not reach or authenticate to. This is the
	// code worth retrying, and the one worth distinguishing: it means no phase did any work, so
	// a scripted retry is safe in a way that a mid-apply failure is not.
	ExitHostUnreachable = 5
	// ExitInterrupted is a run stopped by SIGINT, following the conventional 128 + signal.
	ExitInterrupted = 130
)

var (
	// ErrUsage marks an error caused by how the command was invoked rather than by what it was
	// asked to do. Cobra reports these as plain errors, so they are tagged at the two points
	// they arise: the flag error hook, and the argument validators.
	ErrUsage = errors.New("invalid usage")

	// ErrInvalidInput marks a file whose contents cargoship rejected.
	ErrInvalidInput = errors.New("invalid input file")

	// ErrConfirmRequired marks the --confirm gate in requireConfirm.
	ErrConfirmRequired = errors.New("confirmation required")
)

// ExitCode maps an error from Execute to the process exit code. Order matters: the most specific
// classification wins, and anything unrecognised stays at ExitFailure so that a new error type
// cannot silently change what a caller sees.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return ExitOK

	// Checked before the rest because a cancelled context is how an interrupt arrives, and the
	// error it produces is otherwise indistinguishable from an ordinary failure.
	case errors.Is(err, context.Canceled):
		return ExitInterrupted

	case errors.Is(err, ErrUsage):
		return ExitUsage

	case errors.Is(err, ErrConfirmRequired):
		return ExitConfirmRequired

	// phase.ErrConnect is joined in by the Connect phase, and survives the trip up through
	// cluster.HostsError because that keeps each host's error wrapped rather than formatted.
	case errors.Is(err, phase.ErrConnect):
		return ExitHostUnreachable

	// Both the schema failures from validate and a config file the strict decode rejected.
	case errors.Is(err, ErrInvalidInput):
		return ExitInvalidInput
	}

	// validate's own failures are typed rather than sentinel-wrapped, because their messages are
	// what the command exists to print. Both shapes land here: one bad file returns the
	// ValidationError directly, several return a multiValidationError wrapping them.
	var invalid *pkgschema.ValidationError
	if errors.As(err, &invalid) {
		return ExitInvalidInput
	}

	var multi *multiValidationError
	if errors.As(err, &multi) {
		return ExitInvalidInput
	}

	return ExitFailure
}

// tagUsageArgs walks the command tree and wraps each explicit Args validator so that a wrong
// argument count reports ErrUsage, and so exits 2 rather than 1. Doing it in one pass rather than
// at 28 registration sites means a command added later is covered without anybody remembering.
//
// Commands with a nil Args are left alone. Cobra falls back to its own legacyArgs for those, which
// is what produces the "unknown command" error and its "Did you mean this?" suggestion -- that
// suggestion is worth more than the exit code, and reproducing it here would mean copying cobra's
// formatting. The cost is that an unrecognised command exits 1; an unknown *flag* still exits 2
// through the flag error hook, which is the more common typo.
func tagUsageArgs(c *cobra.Command) {
	if c.Args != nil {
		next := c.Args
		c.Args = func(cmd *cobra.Command, args []string) error {
			if err := next(cmd, args); err != nil {
				return fmt.Errorf("%w: %w\n\tusage: %s\n\trun '%s --help' for details",
					ErrUsage, err, cmd.UseLine(), cmd.CommandPath())
			}
			return nil
		}
	}

	for _, sub := range c.Commands() {
		tagUsageArgs(sub)
	}
}
