// Copyright 2023 harbor-cli authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from harbor-cli:
// https://github.com/goharbor/harbor-cli
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

package main

import (
	"context"
	"fmt"
	"strings"
)

func LDFlags(ctx context.Context, version string, commit string) string {
	return strings.TrimSpace(
		fmt.Sprintf(
			"-X github.com/colonel-byte/cargoship/src/config.CLIVersion=%s "+
				"-X github.com/colonel-byte/cargoship/src/config.CLICommit=%s ",
			version,
			commit,
		),
	)
}
