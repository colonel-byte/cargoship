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

package main

import (
	"fmt"
)

// AnsibleRequirements moves every collection pin to the newest release the controller runs
//
// Reads the supported ansible-core range from the collection's meta/runtime.yml, asks Galaxy for
// every published version of each collection in requirements.yml, and pins the newest one whose
// requires_ansible admits that whole range. Where a collection has moved past the controller, the
// newest release is named alongside the pin so the gap is visible rather than silent. Touches the
// network.
//
// This exists because no dependency bot can do it: Renovate's galaxy-collection datasource reads
// created_at and repository from the same endpoint and never surfaces requires_ansible, and
// constraintsFiltering does not cover that datasource. See
// docs/agent/choice-ansible-collection-pins.md.
func (Generate) AnsibleRequirements() error {
	controller, err := controllerRange()
	if err != nil {
		return err
	}
	fmt.Printf("%s: resolving against ansible-core %s\n", ansibleRequirementsPath, controller)

	req, err := readAnsibleRequirements()
	if err != nil {
		return err
	}

	client := galaxyClient()
	changed := false

	for i := range req.Collections {
		c := &req.Collections[i]

		versions, err := galaxyVersions(client, c.Source, c.Name)
		if err != nil {
			return err
		}

		best, newest, err := resolveCollection(versions, controller)
		if err != nil {
			return fmt.Errorf("%s: %s: %w", ansibleRequirementsPath, c.Name, err)
		}

		// Only worth printing when the collection has releases the controller cannot run:
		// it is the difference between "up to date" and "held back", which look the same.
		held := ""
		if newest.Version != best.Version {
			held = fmt.Sprintf(" (%s needs %s)", newest.Version, newest.RequiresAnsible)
		}

		if c.Version == best.Version {
			fmt.Printf("%s: %s %s unchanged%s\n", ansibleRequirementsPath, c.Name, c.Version, held)
			continue
		}

		fmt.Printf("%s: %s %s -> %s%s\n", ansibleRequirementsPath, c.Name, c.Version, best.Version, held)
		c.Version = best.Version
		changed = true
	}

	if !changed {
		return nil
	}
	return req.write()
}
