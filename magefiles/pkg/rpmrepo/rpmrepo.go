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

// Package rpmrepo reads the repodata an RPM repository publishes: the repomd.xml index, the
// primary.xml catalogue it points at, and the version comparison needed to pick a package out of
// it.
//
// Two generators need this. The DNF pin refresh reads AlmaLinux's repodata for the versions the
// container images install, and the upstream example generator reads pkgs.k8s.io's and
// download.docker.com's for the versions an example pins. Version comparison lives in version.go.
package rpmrepo

import (
	"compress/gzip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// repomd models the top-level repodata/repomd.xml index.
type repomd struct {
	XMLName xml.Name     `xml:"repomd"`
	Data    []repomdElem `xml:"data"`
}

type repomdElem struct {
	Type     string   `xml:"type,attr"`
	Location Location `xml:"location"`
}

// Location is a file's path within a repository, relative to the directory repodata/ lives under.
type Location struct {
	Href string `xml:"href,attr"`
}

// Primary is a repository's primary.xml: its catalogue of packages.
type Primary struct {
	XMLName  xml.Name  `xml:"metadata"`
	Packages []Package `xml:"package"`
}

// Package is one package in a Primary.
type Package struct {
	Name     string   `xml:"name"`
	Arch     string   `xml:"arch"`
	Version  Version  `xml:"version"`
	Checksum string   `xml:"checksum"`
	Location Location `xml:"location"`
}

// FetchPrimary fetches and decompresses a repo's primary.xml, following its repomd.xml index.
// baseURL is the directory repodata/ lives under; a location href in either document is relative
// to it. It branches on the primary document's extension since pkgs.k8s.io publishes gzip and
// download.docker.com's centos repo publishes zstd.
func FetchPrimary(ctx context.Context, client *http.Client, baseURL string) (*Primary, error) {
	repomdURL := baseURL + "/repodata/repomd.xml"

	var index repomd
	if err := fetchXML(ctx, client, repomdURL, func(r io.Reader) error {
		return xml.NewDecoder(r).Decode(&index)
	}); err != nil {
		return nil, err
	}

	var href string
	for _, d := range index.Data {
		if d.Type == "primary" {
			href = d.Location.Href
			break
		}
	}
	if href == "" {
		return nil, fmt.Errorf("%s: no primary data element", repomdURL)
	}

	primaryURL := baseURL + "/" + href
	var primary Primary
	if err := fetchXML(ctx, client, primaryURL, func(r io.Reader) error {
		decompressed, closeIt, err := decompress(href, r)
		if err != nil {
			return fmt.Errorf("decompressing %s: %w", primaryURL, err)
		}
		defer closeIt()
		return xml.NewDecoder(decompressed).Decode(&primary)
	}); err != nil {
		return nil, err
	}
	return &primary, nil
}

// fetchXML GETs url and hands the body to decode, reporting a transport failure, a non-200, and a
// decode failure with the URL that produced it.
func fetchXML(ctx context.Context, client *http.Client, url string, decode func(io.Reader) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body, nothing to do with a close error

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching %s: %s", url, resp.Status)
	}
	if err := decode(resp.Body); err != nil {
		return fmt.Errorf("decoding %s: %w", url, err)
	}
	return nil
}

// decompress wraps r in the reader the href's extension calls for, returning a close function
// that is always safe to call.
func decompress(href string, r io.Reader) (io.Reader, func(), error) {
	switch {
	case strings.HasSuffix(href, ".gz"):
		gz, err := gzip.NewReader(r)
		if err != nil {
			return nil, nil, err
		}
		// gzip.Reader.Close only reports a checksum mismatch, which the decode above has
		// already run past; the caller defers this and has nowhere to put the error.
		return gz, func() { gz.Close() }, nil //nolint:errcheck // see above
	case strings.HasSuffix(href, ".zst"):
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, nil, err
		}
		return zr, zr.Close, nil
	default:
		return nil, nil, fmt.Errorf("unsupported primary metadata compression for %s", href)
	}
}

// FindLatest returns the newest version of pkgName in primary, whatever its architecture. Used
// for repositories that publish one document per architecture.
func FindLatest(primary *Primary, pkgName string) (*Package, error) {
	newest := newestMatch(primary, func(pkg *Package) bool { return pkg.Name == pkgName })
	if newest == nil {
		return nil, fmt.Errorf("package %q not found in repodata", pkgName)
	}
	return newest, nil
}

// FindLatestArch is FindLatest narrowed to one architecture, for repos (pkgs.k8s.io) whose
// primary.xml covers every architecture in one document.
func FindLatestArch(primary *Primary, pkgName, arch string) (*Package, error) {
	newest := newestMatch(primary, func(pkg *Package) bool {
		return pkg.Name == pkgName && pkg.Arch == arch
	})
	if newest == nil {
		return nil, fmt.Errorf("package %q not found for %s in repodata", pkgName, arch)
	}
	return newest, nil
}

// newestMatch returns the highest-versioned package satisfying keep, or nil for no match.
func newestMatch(primary *Primary, keep func(*Package) bool) *Package {
	var newest *Package
	for i := range primary.Packages {
		pkg := &primary.Packages[i]
		if !keep(pkg) {
			continue
		}
		if newest == nil || pkg.Version.Compare(newest.Version) > 0 {
			newest = pkg
		}
	}
	return newest
}

// PickArch is FindLatestArch narrowed to versions at or before target, for backfilling a
// specific tag rather than always rendering the minor line's newest packages.
func PickArch(primary *Primary, pkgName, arch, target string) (*Package, error) {
	var best *Package
	for i := range primary.Packages {
		pkg := &primary.Packages[i]
		if pkg.Name != pkgName || pkg.Arch != arch {
			continue
		}
		if CompareValues(pkg.Version.Ver, target) > 0 {
			continue
		}
		if best == nil || CompareValues(pkg.Version.Ver, best.Version.Ver) > 0 {
			best = pkg
		}
	}
	if best == nil {
		return nil, fmt.Errorf("package %q not found for %s at or before %s in repodata", pkgName, arch, target)
	}
	return best, nil
}
