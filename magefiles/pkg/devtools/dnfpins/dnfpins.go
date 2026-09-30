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

// Package dnfpins queries AlmaLinux 10 repodata over HTTP and updates package pins across Dockerfiles and .goreleaser.yaml.
package dnfpins

import (
	"compress/gzip"
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	AlmaLinuxBaseURL      = "https://repo.almalinux.org/almalinux/10"
	AnsibleDockerfilePath = "containers/ansible/Dockerfile"
	UbiDockerfilePath     = "containers/ubi/Dockerfile"
	GoreleaserConfigPath  = ".goreleaser.yaml"
)

// RepomdXML models the top-level repodata/repomd.xml index.
type RepomdXML struct {
	XMLName xml.Name         `xml:"repomd"`
	Data    []RepomdDataElem `xml:"data"`
}

type RepomdDataElem struct {
	Type     string         `xml:"type,attr"`
	Location RepomdLocation `xml:"location"`
}

type RepomdLocation struct {
	Href string `xml:"href,attr"`
}

// PrimaryXML represents metadata xml packages.
type PrimaryXML struct {
	XMLName  xml.Name     `xml:"metadata"`
	Packages []RpmPackage `xml:"package"`
}

type RpmPackage struct {
	Name     string         `xml:"name"`
	Arch     string         `xml:"arch"`
	Version  RpmVersion     `xml:"version"`
	Checksum string         `xml:"checksum"`
	Location RepomdLocation `xml:"location"`
}

type RpmVersion struct {
	Epoch string `xml:"epoch,attr"`
	Ver   string `xml:"ver,attr"`
	Rel   string `xml:"rel,attr"`
}

// FullVersion returns the version-release representation used for package specification: "<ver>-<rel>".
func (v RpmVersion) FullVersion() string {
	return fmt.Sprintf("%s-%s", v.Ver, v.Rel)
}

// Compare compares RpmVersion to other using standard RPM EVR comparison semantics.
// Returns -1 if v < other, 0 if v == other, 1 if v > other.
func (v RpmVersion) Compare(other RpmVersion) int {
	vEpoch, _ := strconv.Atoi(v.Epoch)
	oEpoch, _ := strconv.Atoi(other.Epoch)
	if vEpoch != oEpoch {
		if vEpoch < oEpoch {
			return -1
		}
		return 1
	}

	cmpVer := RpmCompareValues(v.Ver, other.Ver)
	if cmpVer != 0 {
		return cmpVer
	}

	return RpmCompareValues(v.Rel, other.Rel)
}

// FetchPrimaryXML fetches and decodes the primary.xml.gz from a given repository component.
func FetchPrimaryXML(ctx context.Context, client *http.Client, repo string) (*PrimaryXML, error) {
	repomdURL := fmt.Sprintf("%s/%s/x86_64/os/repodata/repomd.xml", AlmaLinuxBaseURL, repo)
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

	var repomd RepomdXML
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

	primaryURL := fmt.Sprintf("%s/%s/x86_64/os/%s", AlmaLinuxBaseURL, repo, primaryHref)
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

	var primary PrimaryXML
	if err := xml.NewDecoder(gzReader).Decode(&primary); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", primaryURL, err)
	}

	return &primary, nil
}

// FindLatestRPM searches PrimaryXML for the newest version of pkgName.
func FindLatestRPM(primary *PrimaryXML, pkgName string) (*RpmPackage, error) {
	var newest *RpmPackage
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

type AlmaLinuxPins struct {
	AnsibleCore    string
	BashCompletion string
	ShadowUtils    string
}

// QueryLatestAlmaLinuxPackages finds latest versions of ansible-core, bash-completion, and shadow-utils.
func QueryLatestAlmaLinuxPackages(ctx context.Context) (*AlmaLinuxPins, error) {
	client := &http.Client{Timeout: 90 * time.Second}

	appstreamPrimary, err := FetchPrimaryXML(ctx, client, "AppStream")
	if err != nil {
		return nil, fmt.Errorf("AppStream repodata: %w", err)
	}

	ansiblePkg, err := FindLatestRPM(appstreamPrimary, "ansible-core")
	if err != nil {
		return nil, err
	}

	baseosPrimary, err := FetchPrimaryXML(ctx, client, "BaseOS")
	if err != nil {
		return nil, fmt.Errorf("BaseOS repodata: %w", err)
	}

	bashPkg, err := FindLatestRPM(baseosPrimary, "bash-completion")
	if err != nil {
		return nil, err
	}

	shadowPkg, err := FindLatestRPM(baseosPrimary, "shadow-utils")
	if err != nil {
		return nil, err
	}

	return &AlmaLinuxPins{
		AnsibleCore:    ansiblePkg.Version.FullVersion(),
		BashCompletion: bashPkg.Version.FullVersion(),
		ShadowUtils:    shadowPkg.Version.FullVersion(),
	}, nil
}
