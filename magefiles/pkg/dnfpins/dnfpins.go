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

// Package dnfpins keeps the RPM versions the container images install pinned to the newest
// AlmaLinux 10 publishes.
//
// The pins appear in three files -- two Dockerfiles and the goreleaser config that passes the
// same versions as build args -- and they have to agree, so one target queries the repodata once
// and rewrites all three. Rewriting is by regexp against the checked-in text rather than by
// reserialising, so a Dockerfile keeps its comments and a YAML file keeps its formatting.
package dnfpins

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/colonel-byte/cargoship/magefiles/pkg/rpmrepo"
)

const (
	// almalinuxBaseURL is the release the images build on.
	almalinuxBaseURL = "https://repo.almalinux.org/almalinux/10"

	// AnsibleDockerfile is the ansible image Update rewrites the dnf pins in.
	AnsibleDockerfile = "containers/ansible/Dockerfile"
	// UBIDockerfile is the UBI image Update rewrites the dnf pins in.
	UBIDockerfile = "containers/ubi/Dockerfile"
	// GoreleaserConfig is the release config Update rewrites the nfpm dependency pins in.
	GoreleaserConfig = ".goreleaser.yaml"
)

// Pins is one set of package versions, as "<ver>-<rel>" strings ready to hand to dnf.
type Pins struct {
	AnsibleCore    string
	BashCompletion string
	ShadowUtils    string
}

// Update queries AlmaLinux for the newest versions and writes them into all three files,
// reporting each one as it lands so a failure part-way through says what did change.
func Update(ctx context.Context) error {
	fmt.Println("Querying AlmaLinux 10 repodata for latest package versions...")
	pins, err := QueryLatest(ctx)
	if err != nil {
		return fmt.Errorf("querying AlmaLinux packages: %w", err)
	}

	fmt.Printf("Discovered versions:\n  ansible-core:    %s\n  bash-completion: %s\n  shadow-utils:    %s\n",
		pins.AnsibleCore, pins.BashCompletion, pins.ShadowUtils)

	if err := UpdateAnsibleDockerfile(AnsibleDockerfile, pins.AnsibleCore, pins.BashCompletion); err != nil {
		return fmt.Errorf("updating %s: %w", AnsibleDockerfile, err)
	}
	fmt.Printf("Updated %s\n", AnsibleDockerfile)

	if err := UpdateUBIDockerfile(UBIDockerfile, pins.ShadowUtils, pins.BashCompletion); err != nil {
		return fmt.Errorf("updating %s: %w", UBIDockerfile, err)
	}
	fmt.Printf("Updated %s\n", UBIDockerfile)

	if err := UpdateGoreleaser(GoreleaserConfig, pins); err != nil {
		return fmt.Errorf("updating %s: %w", GoreleaserConfig, err)
	}
	fmt.Printf("Updated %s\n", GoreleaserConfig)

	return nil
}

// QueryLatest finds the latest versions of ansible-core, bash-completion, and shadow-utils.
func QueryLatest(ctx context.Context) (*Pins, error) {
	// AlmaLinux's primary.xml runs to tens of megabytes, so the timeout covers the download
	// rather than just the handshake.
	client := &http.Client{Timeout: 90 * time.Second}

	appstream, err := rpmrepo.FetchPrimary(ctx, client, almalinuxBaseURL+"/AppStream/x86_64/os")
	if err != nil {
		return nil, fmt.Errorf("AppStream repodata: %w", err)
	}

	ansiblePkg, err := rpmrepo.FindLatest(appstream, "ansible-core")
	if err != nil {
		return nil, err
	}

	baseos, err := rpmrepo.FetchPrimary(ctx, client, almalinuxBaseURL+"/BaseOS/x86_64/os")
	if err != nil {
		return nil, fmt.Errorf("BaseOS repodata: %w", err)
	}

	bashPkg, err := rpmrepo.FindLatest(baseos, "bash-completion")
	if err != nil {
		return nil, err
	}

	shadowPkg, err := rpmrepo.FindLatest(baseos, "shadow-utils")
	if err != nil {
		return nil, err
	}

	return &Pins{
		AnsibleCore:    ansiblePkg.Version.FullVersion(),
		BashCompletion: bashPkg.Version.FullVersion(),
		ShadowUtils:    shadowPkg.Version.FullVersion(),
	}, nil
}

// UpdateAnsibleDockerfile updates the ARG lines in containers/ansible/Dockerfile.
func UpdateAnsibleDockerfile(path, ansibleVer, bashVer string) error {
	return rewrite(path, map[string]string{
		"ANSIBLE_CORE_VERSION":    ansibleVer,
		"BASH_COMPLETION_VERSION": bashVer,
	})
}

// UpdateUBIDockerfile updates the ARG lines in containers/ubi/Dockerfile.
func UpdateUBIDockerfile(path, shadowVer, bashVer string) error {
	return rewrite(path, map[string]string{
		"SHADOW_UTILS_VERSION":    shadowVer,
		"BASH_COMPLETION_VERSION": bashVer,
	})
}

// rewrite replaces each named ARG's value in a Dockerfile. An ARG that is not there is an error
// rather than a silent no-op: it means the Dockerfile stopped pinning something this target
// believes it pins, and the version would then only be updated in two of the three files.
func rewrite(path string, args map[string]string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(content)

	// Sorted so the error a caller sees does not depend on map iteration order.
	for _, name := range sortedKeys(args) {
		re := regexp.MustCompile(fmt.Sprintf(`(?m)^ARG %s="[^"]*"`, name))
		if !re.MatchString(text) {
			return fmt.Errorf("did not find ARG %s in %s", name, path)
		}
		text = re.ReplaceAllString(text, fmt.Sprintf(`ARG %s="%s"`, name, args[name]))
	}

	return os.WriteFile(path, []byte(text), 0o644)
}

// UpdateGoreleaser updates build_args in .goreleaser.yaml for both cargoship-ubi and
// cargoship-ansible.
//
// Each pattern is anchored on its own image id and reaches forward to that image's build_args, so
// the two blocks cannot be confused for one another even though they share a
// BASH_COMPLETION_VERSION key.
func UpdateGoreleaser(path string, pins *Pins) error {
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

// sortedKeys returns m's keys in a stable order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
