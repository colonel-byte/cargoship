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

package cluster

import (
	"fmt"
	"sort"
	"strings"
)

// ResolveProfiles returns the profiles with every inherit chain folded in, so that nothing
// downstream has to know inheritance exists. A profile without an inherit comes back unchanged.
//
// A host selects one profile, and a profile's settings replace the host's only where the host left
// them empty -- so a cluster whose profiles differ in one field each had to spell every one of them
// out in full, and the profiles block grew with the number of combinations rather than with the
// fleet. Inheritance is the way to say the difference instead of the whole.
//
// The fold uses the same Merge the profile-to-host step uses, in the same direction: the inheriting
// profile is the one with the settings, and the one it names fills in what is still empty. A chain
// is therefore walked child first, and the nearest ancestor to set a field is the one that wins.
// Concurrency is a plain string and follows the same rule by hand, there being no Merge for it.
//
// Two documents are refused rather than resolved. An inherit naming a profile the cluster does not
// define is a typo whose only other symptom is a profile quietly missing half its settings, and a
// cycle has no fold at all. Both name the profiles involved.
func ResolveProfiles(profiles map[string]Profiles) (map[string]Profiles, error) {
	if len(profiles) == 0 {
		return profiles, nil
	}

	resolved := make(map[string]Profiles, len(profiles))
	// Names are walked in a fixed order so that a document with two bad chains in it reports the
	// same one on every run.
	for _, name := range sortedProfileNames(profiles) {
		out, err := foldProfile(name, profiles)
		if err != nil {
			return nil, err
		}
		resolved[name] = out
	}
	return resolved, nil
}

// foldProfile walks one profile's inherit chain and returns the folded result.
func foldProfile(name string, profiles map[string]Profiles) (Profiles, error) {
	out := profiles[name]

	// seen records the chain walked so far, in order, so a cycle can be reported as the path that
	// closes it rather than as the single name it was noticed at.
	seen := []string{name}
	inChain := map[string]bool{name: true}

	for parent := out.Inherit; parent != ""; {
		if inChain[parent] {
			return Profiles{}, fmt.Errorf(
				"profile %q inherits in a cycle: %s -> %s", name, strings.Join(seen, " -> "), parent)
		}
		next, ok := profiles[parent]
		if !ok {
			return Profiles{}, fmt.Errorf(
				"profile %q inherits %q, which the cluster does not define", seen[len(seen)-1], parent)
		}
		seen = append(seen, parent)
		inChain[parent] = true

		// Merge copies only into fields that are still empty, so the nearer profile keeps what it
		// set and this fills in the rest.
		out.Host.Merge(next.Host)
		out.Engine.Merge(next.Engine)
		if out.Concurrency == "" {
			out.Concurrency = next.Concurrency
		}

		parent = next.Inherit
	}

	// The chain is folded in, so the field that named it has nothing left to say. Clearing it
	// keeps a resolved document from reading as though it still had work to do.
	out.Inherit = ""
	return out, nil
}

// sortedProfileNames returns a profile map's keys in a fixed order.
func sortedProfileNames(profiles map[string]Profiles) []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
