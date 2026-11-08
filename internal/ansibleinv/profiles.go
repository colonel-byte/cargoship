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

package ansibleinv

import (
	"fmt"
	"reflect"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
)

// collectProfiles returns the cluster's profiles: the ones the playbook declared, plus the ones
// the hosts declared for themselves through cargoship_profile_spec.
//
// A host already says which profile it takes, with cargoship_profile, and that variable belongs in
// the group_vars of the group that carries the profile -- which is where an operator puts it.
// Until now the body of that profile had to be written somewhere else entirely, in the playbook's
// cluster block, so adding a node group meant editing two distant files and a disagreement between
// them was a run that failed well after it started. cargoship_profile_spec lets the body live
// beside the selection, so group_vars/controller.yml is the whole truth about controllers.
//
// Two rules govern the result, and both are the ones this package already applies elsewhere.
//
// A profile defined twice with different bodies is an error naming both sources. That is the same
// refusal deriveRoles makes for a host in two groups that disagree about its role: there is no
// correct body to pick, and picking one silently would install nodes configured in a way nobody
// wrote down. Two definitions that agree are not a conflict, because there is nothing to lose
// between them -- which is what makes it safe to repeat a spec in several group_vars files.
//
// A host that names no profile of its own still gets one, named for its role, because hostFromVars
// defaults it that way. A spec on such a host therefore defines the profile for its whole role,
// which is the behaviour an operator writing it in group_vars/worker.yml is asking for.
func collectProfiles(
	assignments []assignment,
	hostVars map[string]map[string]any,
	declared map[string]cluster.Profiles,
) (map[string]cluster.Profiles, error) {
	// source records where a profile's body came from, so a conflict can name both sides rather
	// than only saying that one exists.
	type source struct {
		body cluster.Profiles
		from string
	}

	found := make(map[string]source, len(declared))
	for name, body := range declared {
		found[name] = source{body: body, from: "the cluster block"}
	}

	for _, a := range assignments {
		vars := hostVars[a.Host]
		if _, ok := vars[VarProfileSpec]; !ok {
			continue
		}

		var body cluster.Profiles
		if err := decodeVar(a.Host, vars, VarProfileSpec, &body); err != nil {
			return nil, err
		}

		// The name the spec lands under is the one the host selects, resolved the same way
		// hostFromVars resolves it, so the two cannot disagree about which profile is being
		// described.
		name, err := stringVar(a.Host, vars, VarProfile)
		if err != nil {
			return nil, err
		}
		if name == "" {
			name = a.Role
		}

		next := source{body: body, from: fmt.Sprintf("host %q", a.Host)}
		previous, seen := found[name]
		if seen {
			if !reflect.DeepEqual(previous.body, next.body) {
				return nil, fmt.Errorf(
					"profile %q is defined by %s and by %s, and the two do not match: a profile has one body",
					name, previous.from, next.from)
			}
			continue
		}
		found[name] = next
	}

	if len(found) == 0 {
		// Nothing declared and nothing collected. Returning nil rather than an empty map keeps
		// the generated document free of an empty profiles key it would otherwise carry.
		return nil, nil
	}

	out := make(map[string]cluster.Profiles, len(found))
	for name, s := range found {
		out[name] = s.body
	}
	return out, nil
}
