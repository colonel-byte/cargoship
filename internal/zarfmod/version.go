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

package zarfmod

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// MinZarfVersion is the oldest zarf the modules support.
//
// It is where `zarf init` gained its positional PACKAGE_SOURCE argument: before v0.72.0 the only
// way to choose an init package was to run zarf in the directory holding a file named after zarf's
// own version, and the wrapper did exactly that. Taking the package as an argument is what lets an
// operator stage a package under any name, in any directory, or hold it in a registry -- so the
// floor buys the parameter rather than merely tidying it.
const MinZarfVersion = "0.72.0"

// ErrZarfTooOld is returned when the zarf on the machine predates MinZarfVersion.
var ErrZarfTooOld = errors.New("the zarf binary is too old for this module")

// zarfVersion runs `zarf version` and returns what it printed, trimmed.
//
// Zarf writes the version to stdout as a bare string. The runner reports lines rather than a
// buffer, so the first non-empty line is the answer: a zarf built from source prints build detail
// on the lines after it.
func zarfVersion(ctx context.Context, run Runner, binary string) (string, error) {
	var version string
	res, err := run.Run(ctx, Invocation{
		Binary: binary,
		Args:   []string{"version"},
		OnLine: func(line string) {
			if version != "" {
				return
			}
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				version = trimmed
			}
		},
	})
	if err != nil {
		return "", fmt.Errorf("unable to run %s version: %w", binary, err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("%s version exited %d", binary, res.ExitCode)
	}
	if version == "" {
		return "", fmt.Errorf("%s version printed nothing", binary)
	}
	return version, nil
}

// requireZarfVersion checks the zarf on the machine against MinZarfVersion and returns the version
// it found.
//
// A version it cannot read is not a failure. The check exists to turn one confusing error -- an
// old zarf rejecting an argument it has never heard of, reported as a usage error about the wrong
// thing -- into a clear one, and refusing to run against a zarf whose version string is merely
// unfamiliar would be a worse failure than the one being prevented. The unreadable version comes
// back in the result instead, with no error, so a run that then fails has the evidence attached.
func requireZarfVersion(ctx context.Context, run Runner, binary string) (string, error) {
	version, err := zarfVersion(ctx, run, binary)
	if err != nil {
		return "", nil //nolint:nilerr // an unreadable version is reported, not refused: see above
	}

	found, err := semver.NewVersion(strings.TrimPrefix(version, "v"))
	if err != nil {
		return version, nil
	}
	minimum, err := semver.NewVersion(MinZarfVersion)
	if err != nil {
		return version, nil
	}

	if found.LessThan(minimum) {
		return version, fmt.Errorf(
			"%w: %s is %s, and the init_package parameter needs the positional package source zarf added in v%s",
			ErrZarfTooOld, binary, version, MinZarfVersion,
		)
	}
	return version, nil
}
