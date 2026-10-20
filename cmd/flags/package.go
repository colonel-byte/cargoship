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

	"github.com/spf13/cobra"
)

// RegisterOCIConcurrency provides shell completion suggestions for the OCI concurrency flag.
func RegisterOCIConcurrency(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return getOCIConcurrency(), cobra.ShellCompDirectiveNoFileComp
}

const (
	ociConcurrencyMinimumDescription = "minimum: safest for rate-limited or unstable registries"
	ociConcurrencyDefaultDescription = "default: balanced for most registries"
	ociConcurrencyMaximumDescription = "recommended upper limit: risks registry throttling above this"
)

func getOCIConcurrency() []string {
	return []string{
		fmt.Sprintf("%d\t%s", 1, ociConcurrencyMinimumDescription),
		fmt.Sprintf("%d\t%s", 6, ociConcurrencyDefaultDescription),
		fmt.Sprintf("%d\t%s", 10, ociConcurrencyMaximumDescription),
	}
}
