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
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/colonel-byte/cargoship/magefiles/pkg/devtools/dnfpins"
)

// debStanza is one package+architecture entry of a Debian-style Packages index.
type debStanza struct {
	Package      string
	Version      string
	Architecture string
	Filename     string
	SHA256       string
}

// parseDebPackages parses a Debian-control Packages file (pkgs.k8s.io, download.docker.com)
// into its stanzas, one per package+architecture, in file order.
func parseDebPackages(body []byte) []debStanza {
	var stanzas []debStanza
	cur := debStanza{}
	flush := func() {
		if cur.Package != "" {
			stanzas = append(stanzas, cur)
		}
		cur = debStanza{}
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch key {
		case "Package":
			cur.Package = val
		case "Version":
			cur.Version = val
		case "Architecture":
			cur.Architecture = val
		case "Filename":
			cur.Filename = val
		case "SHA256":
			cur.SHA256 = val
		}
	}
	flush()
	return stanzas
}

// fetchDebPackages GETs and parses a Packages index.
func fetchDebPackages(url string) ([]debStanza, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // body is read to completion, nothing to do with a close error
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	return parseDebPackages(body), nil
}

// newestDebStanza picks the newest published stanza for pkg+arch out of a Packages index. A
// Packages file can hold more than one version of the same package while a release is rolling
// out (download.docker.com does for containerd.io), so the first match is not always the right
// one.
func newestDebStanza(stanzas []debStanza, pkg, arch string) (debStanza, error) {
	var best debStanza
	found := false
	for _, s := range stanzas {
		if s.Package != pkg || s.Architecture != arch {
			continue
		}
		if !found || dnfpins.RpmCompareValues(s.Version, best.Version) > 0 {
			best = s
			found = true
		}
	}
	if !found {
		return debStanza{}, fmt.Errorf("no %s package for %s in Packages index", pkg, arch)
	}
	return best, nil
}

// debBaseVersion strips a Debian package version's "-<debianRevision>" suffix, e.g.
// "1.37.0-1.1" -> "1.37.0".
func debBaseVersion(v string) string {
	base, _, _ := strings.Cut(v, "-")
	return base
}

// pickDebStanza picks the closest published stanza for pkg+arch whose version is at or before
// target out of a Packages index. pkgs.k8s.io keeps every patch's packages on a minor line
// rather than only the newest, so backfilling a specific tag has to select that tag's own
// package rather than always the newest one on the line.
func pickDebStanza(stanzas []debStanza, pkg, arch, target string) (debStanza, error) {
	var best debStanza
	found := false
	for _, s := range stanzas {
		if s.Package != pkg || s.Architecture != arch {
			continue
		}
		base := debBaseVersion(s.Version)
		if dnfpins.RpmCompareValues(base, target) > 0 {
			continue
		}
		if !found || dnfpins.RpmCompareValues(base, debBaseVersion(best.Version)) > 0 {
			best = s
			found = true
		}
	}
	if !found {
		return debStanza{}, fmt.Errorf("no %s package for %s at or before %s in Packages index", pkg, arch, target)
	}
	return best, nil
}
