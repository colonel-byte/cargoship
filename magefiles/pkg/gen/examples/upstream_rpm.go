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

package examples

import (
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/colonel-byte/cargoship/magefiles/pkg/devtools/dnfpins"
	"github.com/klauspost/compress/zstd"
)

// fetchRPMPrimary fetches and decompresses a repo's primary.xml, following its repomd.xml
// index. baseURL is the directory repodata/ lives under; a location href in either document is
// relative to it. Branches on the primary document's extension since pkgs.k8s.io publishes gzip
// and download.docker.com's centos repo publishes zstd.
func fetchRPMPrimary(baseURL string) (*dnfpins.PrimaryXML, error) {
	repomdURL := baseURL + "/repodata/repomd.xml"
	resp, err := http.Get(repomdURL)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", repomdURL, err)
	}
	defer resp.Body.Close() //nolint:errcheck // body is read to completion, nothing to do with a close error
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", repomdURL, resp.Status)
	}

	var repomd dnfpins.RepomdXML
	if err := xml.NewDecoder(resp.Body).Decode(&repomd); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", repomdURL, err)
	}

	var href string
	for _, d := range repomd.Data {
		if d.Type == "primary" {
			href = d.Location.Href
			break
		}
	}
	if href == "" {
		return nil, fmt.Errorf("%s: no primary data element", repomdURL)
	}

	primaryURL := baseURL + "/" + href
	presp, err := http.Get(primaryURL)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", primaryURL, err)
	}
	defer presp.Body.Close() //nolint:errcheck // body is read to completion, nothing to do with a close error
	if presp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", primaryURL, presp.Status)
	}

	var r io.Reader
	switch {
	case strings.HasSuffix(href, ".gz"):
		gz, err := gzip.NewReader(presp.Body)
		if err != nil {
			return nil, fmt.Errorf("decompressing %s: %w", primaryURL, err)
		}
		defer gz.Close() //nolint:errcheck // decompressor is read to completion, nothing to do with a close error
		r = gz
	case strings.HasSuffix(href, ".zst"):
		zr, err := zstd.NewReader(presp.Body)
		if err != nil {
			return nil, fmt.Errorf("decompressing %s: %w", primaryURL, err)
		}
		defer zr.Close()
		r = zr
	default:
		return nil, fmt.Errorf("%s: unsupported primary metadata compression", primaryURL)
	}

	var primary dnfpins.PrimaryXML
	if err := xml.NewDecoder(r).Decode(&primary); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", primaryURL, err)
	}
	return &primary, nil
}

// findLatestRPMArch is findLatestRPM narrowed to one architecture, for repos (pkgs.k8s.io) whose
// primary.xml covers every architecture in one document.
func findLatestRPMArch(primary *dnfpins.PrimaryXML, pkgName, arch string) (*dnfpins.RpmPackage, error) {
	var newest *dnfpins.RpmPackage
	for i := range primary.Packages {
		pkg := &primary.Packages[i]
		if pkg.Name != pkgName || pkg.Arch != arch {
			continue
		}
		if newest == nil || pkg.Version.Compare(newest.Version) > 0 {
			newest = pkg
		}
	}
	if newest == nil {
		return nil, fmt.Errorf("package %q not found for %s in repodata", pkgName, arch)
	}
	return newest, nil
}

// pickRPMArch is findLatestRPMArch narrowed to versions at or before target, for backfilling a
// specific tag rather than always rendering the minor line's newest packages.
func pickRPMArch(primary *dnfpins.PrimaryXML, pkgName, arch, target string) (*dnfpins.RpmPackage, error) {
	var best *dnfpins.RpmPackage
	for i := range primary.Packages {
		pkg := &primary.Packages[i]
		if pkg.Name != pkgName || pkg.Arch != arch {
			continue
		}
		if dnfpins.RpmCompareValues(pkg.Version.Ver, target) > 0 {
			continue
		}
		if best == nil || dnfpins.RpmCompareValues(pkg.Version.Ver, best.Version.Ver) > 0 {
			best = pkg
		}
	}
	if best == nil {
		return nil, fmt.Errorf("package %q not found for %s at or before %s in repodata", pkgName, arch, target)
	}
	return best, nil
}
