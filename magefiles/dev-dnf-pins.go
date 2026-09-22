// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"compress/gzip"
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"
)

const (
	almalinuxBaseURL      = "https://repo.almalinux.org/almalinux/10"
	ansibleDockerfilePath = "containers/ansible/Dockerfile"
	ubiDockerfilePath     = "containers/ubi/Dockerfile"
	goreleaserConfigPath  = ".goreleaser.yaml"
)

// repomdXML models the top-level repodata/repomd.xml index.
type repomdXML struct {
	XMLName xml.Name         `xml:"repomd"`
	Data    []repomdDataElem `xml:"data"`
}

type repomdDataElem struct {
	Type     string         `xml:"type,attr"`
	Location repomdLocation `xml:"location"`
}

type repomdLocation struct {
	Href string `xml:"href,attr"`
}

// primaryXML represents metadata xml packages.
type primaryXML struct {
	XMLName  xml.Name     `xml:"metadata"`
	Packages []rpmPackage `xml:"package"`
}

type rpmPackage struct {
	Name    string     `xml:"name"`
	Arch    string     `xml:"arch"`
	Version rpmVersion `xml:"version"`
}

type rpmVersion struct {
	Epoch string `xml:"epoch,attr"`
	Ver   string `xml:"ver,attr"`
	Rel   string `xml:"rel,attr"`
}

// FullVersion returns the version-release representation used for package specification: "<ver>-<rel>".
func (v rpmVersion) FullVersion() string {
	return fmt.Sprintf("%s-%s", v.Ver, v.Rel)
}

// Compare compares rpmVersion to other using standard RPM EVR comparison semantics.
// Returns -1 if v < other, 0 if v == other, 1 if v > other.
func (v rpmVersion) Compare(other rpmVersion) int {
	vEpoch, _ := strconv.Atoi(v.Epoch)
	oEpoch, _ := strconv.Atoi(other.Epoch)
	if vEpoch != oEpoch {
		if vEpoch < oEpoch {
			return -1
		}
		return 1
	}

	cmpVer := rpmCompareValues(v.Ver, other.Ver)
	if cmpVer != 0 {
		return cmpVer
	}

	return rpmCompareValues(v.Rel, other.Rel)
}

// rpmCompareValues implements the RPM segment comparison algorithm (rpmvercmp).
func rpmCompareValues(a, b string) int {
	if a == b {
		return 0
	}

	i, j := 0, 0
	lenA, lenB := len(a), len(b)

	for i < lenA && j < lenB {
		for i < lenA && !isAlphaNum(a[i]) && a[i] != '~' && a[i] != '^' {
			i++
		}
		for j < lenB && !isAlphaNum(b[j]) && b[j] != '~' && b[j] != '^' {
			j++
		}

		if (i < lenA && a[i] == '~') || (j < lenB && b[j] == '~') {
			if i < lenA && a[i] == '~' && (j >= lenB || b[j] != '~') {
				return -1
			}
			if j < lenB && b[j] == '~' && (i >= lenA || a[i] != '~') {
				return 1
			}
			i++
			j++
			continue
		}

		if (i < lenA && a[i] == '^') || (j < lenB && b[j] == '^') {
			if i < lenA && a[i] == '^' && (j >= lenB || b[j] != '^') {
				return -1
			}
			if j < lenB && b[j] == '^' && (i >= lenA || a[i] != '^') {
				return 1
			}
			i++
			j++
			continue
		}

		if i >= lenA || j >= lenB {
			break
		}

		if isDigit(a[i]) {
			startA := i
			for i < lenA && isDigit(a[i]) {
				i++
			}
			for startA < i-1 && a[startA] == '0' {
				startA++
			}

			startB := j
			for j < lenB && isDigit(b[j]) {
				j++
			}
			for startB < j-1 && b[startB] == '0' {
				startB++
			}

			segA := a[startA:i]
			segB := b[startB:j]

			if len(segA) != len(segB) {
				if len(segA) < len(segB) {
					return -1
				}
				return 1
			}
			if segA != segB {
				if segA < segB {
					return -1
				}
				return 1
			}
		} else if isAlpha(a[i]) {
			startA := i
			for i < lenA && isAlpha(a[i]) {
				i++
			}
			startB := j
			for j < lenB && isAlpha(b[j]) {
				j++
			}

			segA := a[startA:i]
			segB := b[startB:j]

			if segA != segB {
				if segA < segB {
					return -1
				}
				return 1
			}
		} else {
			i++
			j++
		}
	}

	if i >= lenA && j >= lenB {
		return 0
	}
	if i >= lenA {
		if j < lenB && b[j] == '~' {
			return 1
		}
		return -1
	}
	if a[i] == '~' {
		return -1
	}
	return 1
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isAlphaNum(c byte) bool {
	return isDigit(c) || isAlpha(c)
}

// fetchPrimaryXML fetches and decodes the primary.xml.gz from a given repository component.
func fetchPrimaryXML(ctx context.Context, client *http.Client, repo string) (*primaryXML, error) {
	repomdURL := fmt.Sprintf("%s/%s/x86_64/os/repodata/repomd.xml", almalinuxBaseURL, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, repomdURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", repomdURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: unexpected HTTP %d", repomdURL, resp.StatusCode)
	}

	var repomd repomdXML
	if err := xml.NewDecoder(resp.Body).Decode(&repomd); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", repomdURL, err)
	}

	var primaryHref string
	for _, d := range repomd.Data {
		if d.Type == "primary" {
			primaryHref = d.Location.Href
			break
		}
	}
	if primaryHref == "" {
		return nil, fmt.Errorf("primary data element not found in %s", repomdURL)
	}

	primaryURL := fmt.Sprintf("%s/%s/x86_64/os/%s", almalinuxBaseURL, repo, primaryHref)
	pReq, err := http.NewRequestWithContext(ctx, http.MethodGet, primaryURL, nil)
	if err != nil {
		return nil, err
	}

	pResp, err := client.Do(pReq)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", primaryURL, err)
	}
	defer pResp.Body.Close()

	if pResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: unexpected HTTP %d", primaryURL, pResp.StatusCode)
	}

	gzReader, err := gzip.NewReader(pResp.Body)
	if err != nil {
		return nil, fmt.Errorf("decompressing %s: %w", primaryURL, err)
	}
	defer gzReader.Close()

	var primary primaryXML
	if err := xml.NewDecoder(gzReader).Decode(&primary); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", primaryURL, err)
	}

	return &primary, nil
}

// findLatestRPM searches primaryXML for the newest version of pkgName.
func findLatestRPM(primary *primaryXML, pkgName string) (*rpmPackage, error) {
	var newest *rpmPackage
	for i := range primary.Packages {
		pkg := &primary.Packages[i]
		if pkg.Name != pkgName {
			continue
		}
		if newest == nil || pkg.Version.Compare(newest.Version) > 0 {
			newest = pkg
		}
	}
	if newest == nil {
		return nil, fmt.Errorf("package %q not found in repodata", pkgName)
	}
	return newest, nil
}

type almaLinuxPins struct {
	AnsibleCore    string
	BashCompletion string
	ShadowUtils    string
}

// queryLatestAlmaLinuxPackages finds latest versions of ansible-core, bash-completion, and shadow-utils.
func queryLatestAlmaLinuxPackages(ctx context.Context) (*almaLinuxPins, error) {
	client := &http.Client{Timeout: 90 * time.Second}

	appstreamPrimary, err := fetchPrimaryXML(ctx, client, "AppStream")
	if err != nil {
		return nil, fmt.Errorf("AppStream repodata: %w", err)
	}

	ansiblePkg, err := findLatestRPM(appstreamPrimary, "ansible-core")
	if err != nil {
		return nil, err
	}

	baseosPrimary, err := fetchPrimaryXML(ctx, client, "BaseOS")
	if err != nil {
		return nil, fmt.Errorf("BaseOS repodata: %w", err)
	}

	bashPkg, err := findLatestRPM(baseosPrimary, "bash-completion")
	if err != nil {
		return nil, err
	}

	shadowPkg, err := findLatestRPM(baseosPrimary, "shadow-utils")
	if err != nil {
		return nil, err
	}

	return &almaLinuxPins{
		AnsibleCore:    ansiblePkg.Version.FullVersion(),
		BashCompletion: bashPkg.Version.FullVersion(),
		ShadowUtils:    shadowPkg.Version.FullVersion(),
	}, nil
}

// updateDockerfileAnsiblePins updates the ARG lines in containers/ansible/Dockerfile.
func updateDockerfileAnsiblePins(path, ansibleVer, bashVer string) error {
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

// updateDockerfileUbiPins updates the ARG lines in containers/ubi/Dockerfile.
func updateDockerfileUbiPins(path, shadowVer, bashVer string) error {
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

// updateGoreleaserDnfPins updates build_args in .goreleaser.yaml for both cargoship-ubi and cargoship-ansible.
func updateGoreleaserDnfPins(path string, pins *almaLinuxPins) error {
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
