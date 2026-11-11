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
	"sort"

	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
)

// RegisterKubeContext provides shell completion suggestions for a kubeconfig context flag,
// taken from whichever kubeconfig the loading rules find.
//
// A context name is not something an operator should have to remember or retype, and getting it
// wrong is only discovered as a connection failure against the wrong cluster. The suggestions
// carry the cluster each context points at, because a name alone frequently does not say.
//
// An unreadable or absent kubeconfig yields no suggestions rather than an error: completion runs
// on every tab press, and a shell is not where a configuration problem should be reported.
func RegisterKubeContext(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return getKubeContexts(), cobra.ShellCompDirectiveNoFileComp
}

func getKubeContexts() []string {
	cfg, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	if err != nil || cfg == nil {
		return nil
	}

	out := make([]string, 0, len(cfg.Contexts))
	for name, kctx := range cfg.Contexts {
		// A cluster named the same as its context says nothing twice.
		if kctx == nil || kctx.Cluster == "" || kctx.Cluster == name {
			out = append(out, name)
			continue
		}
		out = append(out, fmt.Sprintf("%s\t%s", name, kctx.Cluster))
	}
	// Map iteration order is random, and a completion list that reorders itself between tab
	// presses is one an operator cannot read.
	sort.Strings(out)
	return out
}
