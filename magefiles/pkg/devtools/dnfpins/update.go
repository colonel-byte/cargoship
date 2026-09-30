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

// This file rewrites the pins in place: the ARG lines in the container Dockerfiles and the dnf
// package list in .goreleaser.yaml. Each rewriter is anchored to the line it replaces so a
// pin update never reformats the file around it.

package dnfpins

import (
	"fmt"
	"os"
	"regexp"
)

// UpdateDockerfileAnsiblePins updates the ARG lines in containers/ansible/Dockerfile.
func UpdateDockerfileAnsiblePins(path, ansibleVer, bashVer string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	text := string(content)

	reAnsible := regexp.MustCompile(`(?m)^ARG ANSIBLE_CORE_VERSION="[^"]*"`)
	if !reAnsible.MatchString(text) {
		return fmt.Errorf("did not find ARG ANSIBLE_CORE_VERSION in %s", path)
	}
	text = reAnsible.ReplaceAllString(text, fmt.Sprintf(`ARG ANSIBLE_CORE_VERSION="%s"`, ansibleVer))

	reBash := regexp.MustCompile(`(?m)^ARG BASH_COMPLETION_VERSION="[^"]*"`)
	if !reBash.MatchString(text) {
		return fmt.Errorf("did not find ARG BASH_COMPLETION_VERSION in %s", path)
	}
	text = reBash.ReplaceAllString(text, fmt.Sprintf(`ARG BASH_COMPLETION_VERSION="%s"`, bashVer))

	return os.WriteFile(path, []byte(text), 0o644)
}

// UpdateDockerfileUbiPins updates the ARG lines in containers/ubi/Dockerfile.
func UpdateDockerfileUbiPins(path, shadowVer, bashVer string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	text := string(content)

	reShadow := regexp.MustCompile(`(?m)^ARG SHADOW_UTILS_VERSION="[^"]*"`)
	if !reShadow.MatchString(text) {
		return fmt.Errorf("did not find ARG SHADOW_UTILS_VERSION in %s", path)
	}
	text = reShadow.ReplaceAllString(text, fmt.Sprintf(`ARG SHADOW_UTILS_VERSION="%s"`, shadowVer))

	reBash := regexp.MustCompile(`(?m)^ARG BASH_COMPLETION_VERSION="[^"]*"`)
	if !reBash.MatchString(text) {
		return fmt.Errorf("did not find ARG BASH_COMPLETION_VERSION in %s", path)
	}
	text = reBash.ReplaceAllString(text, fmt.Sprintf(`ARG BASH_COMPLETION_VERSION="%s"`, bashVer))

	return os.WriteFile(path, []byte(text), 0o644)
}

// UpdateGoreleaserDnfPins updates build_args in .goreleaser.yaml for both cargoship-ubi and cargoship-ansible.
func UpdateGoreleaserDnfPins(path string, pins *AlmaLinuxPins) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	text := string(content)

	// Replace under cargoship-ubi: SHADOW_UTILS_VERSION and BASH_COMPLETION_VERSION
	reUbiBlock := regexp.MustCompile(`(?s)(- id: cargoship-ubi.*?build_args:.*?\n)(\s+SHADOW_UTILS_VERSION:\s*)[^\n]+(\n\s+BASH_COMPLETION_VERSION:\s*)[^\n]+`)
	if !reUbiBlock.MatchString(text) {
		return fmt.Errorf("did not find cargoship-ubi build_args in %s", path)
	}
	text = reUbiBlock.ReplaceAllString(text, fmt.Sprintf("${1}${2}%s${3}%s", pins.ShadowUtils, pins.BashCompletion))

	// Replace under cargoship-ansible: ANSIBLE_CORE_VERSION and BASH_COMPLETION_VERSION
	reAnsibleBlock := regexp.MustCompile(`(?s)(- id: cargoship-ansible.*?build_args:.*?\n)(\s+ANSIBLE_CORE_VERSION:\s*)[^\n]+(\n\s+BASH_COMPLETION_VERSION:\s*)[^\n]+`)
	if !reAnsibleBlock.MatchString(text) {
		return fmt.Errorf("did not find cargoship-ansible build_args in %s", path)
	}
	text = reAnsibleBlock.ReplaceAllString(text, fmt.Sprintf("${1}${2}%s${3}%s", pins.AnsibleCore, pins.BashCompletion))

	return os.WriteFile(path, []byte(text), 0o644)
}
