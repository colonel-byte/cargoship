//go:build mage

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
	"errors"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// AnsibleRequirements checks every collection pin can run on the supported ansible-core
//
// Asks Galaxy what each pinned version declares in requires_ansible and fails when that does not
// admit the whole controller range in the collection's meta/runtime.yml. Touches the network.
//
// Deliberately not "is the pin the newest": an upstream release published this morning must not
// fail an unrelated pull request. Run mage generate:ansibleRequirements to move the pins forward.
func (Test) AnsibleRequirements() error {
	controller, err := controllerRange()
	if err != nil {
		return err
	}

	req, err := readAnsibleRequirements()
	if err != nil {
		return err
	}

	client := galaxyClient()

	var problems []error
	for _, c := range req.Collections {
		versions, err := galaxyVersions(client, c.Source, c.Name)
		if err != nil {
			return err
		}

		pinned, ok := findGalaxyVersion(versions, c.Version)
		if !ok {
			problems = append(problems, fmt.Errorf("%s %s is not published on %s", c.Name, c.Version, c.Source))
			continue
		}

		rng, err := parseSpecifier(pinned.RequiresAnsible)
		if err != nil {
			problems = append(problems, fmt.Errorf("%s %s declares requires_ansible %q: %w",
				c.Name, c.Version, pinned.RequiresAnsible, err))
			continue
		}

		if !rng.admits(controller) {
			problems = append(problems, fmt.Errorf(
				"%s %s supports ansible-core %s, which does not cover %s",
				c.Name, c.Version, rng, controller))
			continue
		}

		fmt.Printf("%s: %s %s supports ansible-core %s\n", ansibleRequirementsPath, c.Name, c.Version, rng)
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s: pins do not support ansible-core %s:\n  %w",
			ansibleRequirementsPath, controller, joinProblems(problems))
	}
	return nil
}

// findGalaxyVersion locates one published version by number. The comparison is on parsed versions
// so a pin written as 6.6 matches a release published as 6.6.0.
func findGalaxyVersion(versions []galaxyVersion, want string) (galaxyVersion, bool) {
	wantVer, err := semver.NewVersion(want)
	if err != nil {
		return galaxyVersion{}, false
	}

	for _, v := range versions {
		ver, err := semver.NewVersion(v.Version)
		if err != nil {
			continue
		}
		if ver.Equal(wantVer) {
			return v, true
		}
	}
	return galaxyVersion{}, false
}

// joinProblems renders every failing pin, one per line, rather than only the first. A pull request
// that moved two pins should learn about both in one run.
func joinProblems(problems []error) error {
	lines := make([]string, len(problems))
	for i, p := range problems {
		lines[i] = p.Error()
	}
	return errors.New(strings.Join(lines, "\n  "))
}
