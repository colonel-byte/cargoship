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
	"slices"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
)

// roleOrder is the order roles are emitted in, and it is not cosmetic: ConfigureEngine makes
// the first controller in the document the leader, so controllers have to come first for the
// leader to be a controller at all.
var roleOrder = []string{cluster.RoleController, cluster.RoleWorker}

// DefaultRoleGroups maps each cargoship role onto an Ansible group of the same name. It names
// cargoship's own roles rather than guessing at an Ansible convention, because there is no one
// convention to guess at: an inventory may call its control plane `control_plane`, `masters`,
// `etcd`, or anything else. An operator whose groups are named otherwise says so.
func DefaultRoleGroups() map[string][]string {
	return map[string][]string{
		cluster.RoleController: {cluster.RoleController},
		cluster.RoleWorker:     {cluster.RoleWorker},
	}
}

// assignment is one host and the role its group membership gave it.
type assignment struct {
	// Host is the inventory hostname, which is the key into hostvars.
	Host string
	// Role is the cargoship role, either controller or worker.
	Role string
	// Group is the Ansible group the role came from, for error messages.
	Group string
}

// deriveRoles walks the role mapping and returns one assignment per host, in the order the
// generated document lists them.
//
// A host in no mapped group is left out rather than refused: a play's inventory may carry hosts
// that have nothing to do with this cluster, and the operator has already said which groups are
// the cluster by writing the mapping. A host in two groups that disagree about its role is an
// error naming the host and both groups, because there is no correct answer to pick and picking
// one silently would install a node in the wrong plane.
func deriveRoles(groups map[string][]string, roleGroups map[string][]string) ([]assignment, error) {
	// A mapping the operator wrote is held to naming groups that exist, because a misspelled
	// group name there is a typo with no other symptom. The default mapping is not: it names
	// both roles, and a cluster of nothing but control-plane nodes has no worker group to name.
	named := len(roleGroups) > 0
	if !named {
		roleGroups = DefaultRoleGroups()
	}
	if err := checkRoles(roleGroups); err != nil {
		return nil, err
	}
	if named {
		if err := checkGroupsExist(groups, roleGroups); err != nil {
			return nil, err
		}
	}

	claimed := make(map[string]assignment)
	out := make([]assignment, 0, len(groups))
	for _, role := range roleOrder {
		for _, group := range roleGroups[role] {
			for _, host := range groups[group] {
				next := assignment{Host: host, Role: role, Group: group}
				previous, seen := claimed[host]
				if seen {
					if previous.Role != next.Role {
						return nil, fmt.Errorf(
							"host %q is in group %q, which maps to role %s, and in group %q, which maps to role %s: a host takes one role",
							host, previous.Group, previous.Role, next.Group, next.Role)
					}
					continue
				}
				claimed[host] = next
				out = append(out, next)
			}
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no host is in any of the groups the role mapping names: nothing to install")
	}
	if !slices.ContainsFunc(out, func(a assignment) bool { return a.Role == cluster.RoleController }) {
		return nil, fmt.Errorf("no host takes the %s role: a cluster needs at least one control-plane node", cluster.RoleController)
	}
	return out, nil
}

// checkRoles refuses a role the inventory schema does not allow. The schema permits controller
// and worker and nothing else, so a mapping naming a third role produces a document that cannot
// be installed; saying so here names the mapping key rather than a schema path.
func checkRoles(roleGroups map[string][]string) error {
	for _, role := range sortedKeys(roleGroups) {
		if !slices.Contains(roleOrder, role) {
			return fmt.Errorf("role mapping names role %q, expected one of %s", role, strings.Join(roleOrder, ", "))
		}
		if len(roleGroups[role]) == 0 {
			return fmt.Errorf("role mapping names role %q with no groups: remove it or name a group", role)
		}
	}
	return nil
}

// checkGroupsExist refuses a mapping that names a group the inventory does not define. Ansible
// reports an undefined group as an empty one, so a typo would otherwise translate into a cluster
// quietly missing every worker.
func checkGroupsExist(groups map[string][]string, roleGroups map[string][]string) error {
	for _, role := range roleOrder {
		for _, group := range roleGroups[role] {
			if _, ok := groups[group]; !ok {
				return fmt.Errorf("role mapping gives role %s the group %q, which the inventory does not define", role, group)
			}
		}
	}
	return nil
}

// sortedKeys returns a map's keys in a fixed order, so that a mapping with two problems in it
// reports the same one on every run.
func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
