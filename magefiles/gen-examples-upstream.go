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

// This file renders example/upstream's distro.yaml examples. Upstream (kubeadm) does not fit
// exampleDistroSpec: it installs five independently-versioned OS packages from pkgs.k8s.io and
// containerd.io from download.docker.com, rather than one release's binaries and image list, so
// it gets its own fetch pipeline and its own call from Generate.Examples rather than a fifth
// exampleDistroSpec entry.

package main

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/klauspost/compress/zstd"
)

const (
	upstreamTemplate = "magefiles/templates/upstream-distro.yaml.tmpl"
	upstreamDir      = "example/upstream"

	// upstreamDebCodename is the Debian release pkgs.k8s.io and download.docker.com both
	// publish packages for. The example targets one OS baseline, so this is a constant
	// rather than a lookup.
	upstreamDebCodename = "trixie"

	// upstreamDockerRPMRelease is the RHEL major release download.docker.com's rpm repo is
	// split by, matching the centos/9 baseline rpm.rancher.io's own examples use.
	upstreamDockerRPMRelease = "9"
)

// upstreamOSPackages is every OS package the example installs, in the order the template
// renders them. containerd.io comes from download.docker.com; the rest come from pkgs.k8s.io.
var upstreamOSPackages = []string{"cri-tools", "kubernetes-cni", "kubeadm", "kubelet", "kubectl"}

// upstreamPackageFile is one architecture's copy of one OS package, ready for a ZarfFile entry.
type upstreamPackageFile struct {
	Package string // kubeadm -- for readability in the rendered comments
	Source  string
	Target  string
	SHA256  string
	Arch    string // amd64 -- what the file's selector.arch names
}

// upstreamArch is one architecture's share of the example: every package file pkgs.k8s.io and
// download.docker.com publish for it, as both a .deb and an .rpm.
type upstreamArch struct {
	Arch    string // amd64
	RPMArch string // x86_64 -- what pkgs.k8s.io and download.docker.com call the same architecture
	Deb     []upstreamPackageFile
	RPM     []upstreamPackageFile
}

// upstreamVersion is what upstream-distro.yaml.tmpl renders against.
//
// Version is kubelet's currently published version rather than the pinned kubernetes/kubernetes
// tag: kubeadm, kubelet, and kubectl are independently versioned OS packages that do not always
// share a patch release (kubeadm trails kubelet by a patch as often as not), and kubelet's
// version is what the control-plane images below have always tracked, so deriving both from the
// same source keeps the example internally consistent instead of quietly drifting apart the way
// the hand-written file did.
type upstreamVersion struct {
	Version string // 1.35.3 -- spec.version, and the registry.k8s.io/kube-*:vX.Y.Z image tag
	Minor   string // 1.35 -- the pkgs.k8s.io URL segment

	EtcdVersion    string // 3.6.6-0
	CoreDNSVersion string // v1.13.1
	PauseVersion   string // 3.10.1

	Arches    []upstreamArch
	MultiArch bool
}

const upstreamRawGithubBase = "https://raw.githubusercontent.com"

var (
	upstreamEtcdVersionPattern    = regexp.MustCompile(`DefaultEtcdVersion\s*=\s*"([^"]+)"`)
	upstreamCoreDNSVersionPattern = regexp.MustCompile(`CoreDNSVersion\s*=\s*"([^"]+)"`)
	upstreamPauseVersionPattern   = regexp.MustCompile(`PauseVersion\s*=\s*"([^"]+)"`)
)

// kubeadmConstants is fetchKubeadmConstants against the real kubernetes/kubernetes repo.
func kubeadmConstants(tag string) (etcd, coredns, pause string, err error) {
	return fetchKubeadmConstants(upstreamRawGithubBase, tag)
}

// fetchKubeadmConstants fetches the etcd, coreDNS, and pause image versions kubeadm pins for a
// release straight out of that tag's own constants.go -- those three images do not track the
// Kubernetes version, and kubeadm's source is the only place that says what a given release
// actually deploys. rawBase is overridable so tests do not need network access.
func fetchKubeadmConstants(rawBase, tag string) (etcd, coredns, pause string, err error) {
	url := fmt.Sprintf("%s/kubernetes/kubernetes/%s/cmd/kubeadm/app/constants/constants.go", rawBase, tag)
	resp, err := http.Get(url)
	if err != nil {
		return "", "", "", fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("fetching %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", fmt.Errorf("reading %s: %w", url, err)
	}

	find := func(re *regexp.Regexp, name string) (string, error) {
		m := re.FindSubmatch(data)
		if m == nil {
			return "", fmt.Errorf("%s: no %s constant found", url, name)
		}
		return string(m[1]), nil
	}

	if etcd, err = find(upstreamEtcdVersionPattern, "DefaultEtcdVersion"); err != nil {
		return "", "", "", err
	}
	if coredns, err = find(upstreamCoreDNSVersionPattern, "CoreDNSVersion"); err != nil {
		return "", "", "", err
	}
	if pause, err = find(upstreamPauseVersionPattern, "PauseVersion"); err != nil {
		return "", "", "", err
	}
	return etcd, coredns, pause, nil
}

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
	defer resp.Body.Close()
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
		if !found || rpmCompareValues(s.Version, best.Version) > 0 {
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
		if rpmCompareValues(base, target) > 0 {
			continue
		}
		if !found || rpmCompareValues(base, debBaseVersion(best.Version)) > 0 {
			best = s
			found = true
		}
	}
	if !found {
		return debStanza{}, fmt.Errorf("no %s package for %s at or before %s in Packages index", pkg, arch, target)
	}
	return best, nil
}

// fetchRPMPrimary fetches and decompresses a repo's primary.xml, following its repomd.xml
// index. baseURL is the directory repodata/ lives under; a location href in either document is
// relative to it. Branches on the primary document's extension since pkgs.k8s.io publishes gzip
// and download.docker.com's centos repo publishes zstd.
func fetchRPMPrimary(baseURL string) (*primaryXML, error) {
	repomdURL := baseURL + "/repodata/repomd.xml"
	resp, err := http.Get(repomdURL)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", repomdURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", repomdURL, resp.Status)
	}

	var repomd repomdXML
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
	defer presp.Body.Close()
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
		defer gz.Close()
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

	var primary primaryXML
	if err := xml.NewDecoder(r).Decode(&primary); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", primaryURL, err)
	}
	return &primary, nil
}

// findLatestRPMArch is findLatestRPM narrowed to one architecture, for repos (pkgs.k8s.io) whose
// primary.xml covers every architecture in one document.
func findLatestRPMArch(primary *primaryXML, pkgName, arch string) (*rpmPackage, error) {
	var newest *rpmPackage
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
func pickRPMArch(primary *primaryXML, pkgName, arch, target string) (*rpmPackage, error) {
	var best *rpmPackage
	for i := range primary.Packages {
		pkg := &primary.Packages[i]
		if pkg.Name != pkgName || pkg.Arch != arch {
			continue
		}
		if rpmCompareValues(pkg.Version.Ver, target) > 0 {
			continue
		}
		if best == nil || rpmCompareValues(pkg.Version.Ver, best.Version.Ver) > 0 {
			best = pkg
		}
	}
	if best == nil {
		return nil, fmt.Errorf("package %q not found for %s at or before %s in repodata", pkgName, arch, target)
	}
	return best, nil
}

// upstreamRepoCache memoizes the four repodata documents a backfill needs -- k8s's deb/rpm
// indexes per minor line, and download.docker.com's deb/rpm indexes per architecture. All four
// are the same for every tag on a line (k8s's cover every patch already published, and
// containerd.io does not vary by k8s tag at all), so without this a backfill of N patches
// downloaded and reparsed each multi-megabyte index N times over.
type upstreamRepoCache struct {
	k8sDeb    map[string][]debStanza
	k8sRPM    map[string]*primaryXML
	dockerDeb map[string][]debStanza
	dockerRPM map[string]*primaryXML
}

func newUpstreamRepoCache() *upstreamRepoCache {
	return &upstreamRepoCache{
		k8sDeb:    map[string][]debStanza{},
		k8sRPM:    map[string]*primaryXML{},
		dockerDeb: map[string][]debStanza{},
		dockerRPM: map[string]*primaryXML{},
	}
}

func (c *upstreamRepoCache) k8sDebStanzas(minor string) ([]debStanza, error) {
	if s, ok := c.k8sDeb[minor]; ok {
		return s, nil
	}
	s, err := fetchDebPackages(upstreamK8sDebBase(minor) + "/Packages")
	if err != nil {
		return nil, err
	}
	c.k8sDeb[minor] = s
	return s, nil
}

func (c *upstreamRepoCache) k8sRPMPrimary(minor string) (*primaryXML, error) {
	if p, ok := c.k8sRPM[minor]; ok {
		return p, nil
	}
	p, err := fetchRPMPrimary(upstreamK8sRPMBase(minor))
	if err != nil {
		return nil, err
	}
	c.k8sRPM[minor] = p
	return p, nil
}

func (c *upstreamRepoCache) dockerDebStanzas(arch string) ([]debStanza, error) {
	if s, ok := c.dockerDeb[arch]; ok {
		return s, nil
	}
	s, err := fetchDebPackages(upstreamDockerDebPackagesURL(arch))
	if err != nil {
		return nil, err
	}
	c.dockerDeb[arch] = s
	return s, nil
}

func (c *upstreamRepoCache) dockerRPMPrimary(rpmArch string) (*primaryXML, error) {
	if p, ok := c.dockerRPM[rpmArch]; ok {
		return p, nil
	}
	p, err := fetchRPMPrimary(upstreamDockerRPMBase(rpmArch))
	if err != nil {
		return nil, err
	}
	c.dockerRPM[rpmArch] = p
	return p, nil
}

// upstreamK8sDebBase is the pkgs.k8s.io deb repo for a minor line ("1.35").
func upstreamK8sDebBase(minor string) string {
	return fmt.Sprintf("https://pkgs.k8s.io/core:/stable:/v%s/deb", minor)
}

// upstreamK8sRPMBase is the pkgs.k8s.io rpm repo for a minor line.
func upstreamK8sRPMBase(minor string) string {
	return fmt.Sprintf("https://pkgs.k8s.io/core:/stable:/v%s/rpm", minor)
}

// upstreamDockerDebPackagesURL is download.docker.com's Packages index for one architecture --
// unlike pkgs.k8s.io, it publishes one file per architecture rather than one file for all of
// them.
func upstreamDockerDebPackagesURL(arch string) string {
	return fmt.Sprintf("https://download.docker.com/linux/debian/dists/%s/stable/binary-%s/Packages", upstreamDebCodename, arch)
}

// upstreamDockerRPMBase is download.docker.com's rpm repo for one architecture.
func upstreamDockerRPMBase(rpmArch string) string {
	return fmt.Sprintf("https://download.docker.com/linux/rhel/%s/%s/stable", upstreamDockerRPMRelease, rpmArch)
}

// fetchUpstreamArch builds one architecture's package file list: every pkgs.k8s.io package as
// both a .deb and an .rpm, plus containerd.io from download.docker.com in the same two forms.
// target is the tag's own version ("1.37.0"): pkgs.k8s.io keeps every patch's packages on the
// minor line, so each tag selects its own package rather than always the newest on the line.
// containerd.io has no such anchor -- its release cadence is independent of the k8s tag -- so it
// still always takes the newest published version. kubeletVersion returns kubelet's
// newly-discovered deb/rpm version, since that is what the caller derives the example's
// Kubernetes version from.
func fetchUpstreamArch(minor, target, arch, rpmArch string, cache *upstreamRepoCache, sums *exampleShasums) (upstreamArch, string, error) {
	a := upstreamArch{Arch: arch, RPMArch: rpmArch}
	debBase := upstreamK8sDebBase(minor)
	rpmBase := upstreamK8sRPMBase(minor)

	debStanzas, err := cache.k8sDebStanzas(minor)
	if err != nil {
		return upstreamArch{}, "", err
	}
	k8sRPMPrimary, err := cache.k8sRPMPrimary(minor)
	if err != nil {
		return upstreamArch{}, "", err
	}

	var kubeletVersion string
	for _, pkg := range upstreamOSPackages {
		deb, err := pickDebStanza(debStanzas, pkg, arch, target)
		if err != nil {
			return upstreamArch{}, "", err
		}
		debSource := debBase + "/" + deb.Filename
		sums.record(debSource, deb.SHA256)
		a.Deb = append(a.Deb, upstreamPackageFile{
			Package: pkg,
			Source:  debSource,
			Target:  "/var/lib/kubernetes/deb/" + path.Base(deb.Filename),
			SHA256:  deb.SHA256,
			Arch:    arch,
		})

		rpmPkg, err := pickRPMArch(k8sRPMPrimary, pkg, rpmArch, target)
		if err != nil {
			return upstreamArch{}, "", err
		}
		rpmSource := rpmBase + "/" + rpmPkg.Location.Href
		sums.record(rpmSource, rpmPkg.Checksum)
		a.RPM = append(a.RPM, upstreamPackageFile{
			Package: pkg,
			Source:  rpmSource,
			Target:  "/var/lib/kubernetes/rpm/" + path.Base(rpmPkg.Location.Href),
			SHA256:  rpmPkg.Checksum,
			Arch:    arch,
		})

		if pkg == "kubelet" {
			kubeletVersion = debBaseVersion(deb.Version)
		}
	}

	dockerDeb, err := cache.dockerDebStanzas(arch)
	if err != nil {
		return upstreamArch{}, "", err
	}
	containerdDeb, err := newestDebStanza(dockerDeb, "containerd.io", arch)
	if err != nil {
		return upstreamArch{}, "", err
	}
	containerdDebSource := "https://download.docker.com/linux/debian/" + containerdDeb.Filename
	sums.record(containerdDebSource, containerdDeb.SHA256)
	a.Deb = append(a.Deb, upstreamPackageFile{
		Package: "containerd.io",
		Source:  containerdDebSource,
		Target:  "/var/lib/kubernetes/deb/" + path.Base(containerdDeb.Filename),
		SHA256:  containerdDeb.SHA256,
		Arch:    arch,
	})

	dockerRPMBase := upstreamDockerRPMBase(rpmArch)
	dockerRPMPrimary, err := cache.dockerRPMPrimary(rpmArch)
	if err != nil {
		return upstreamArch{}, "", err
	}
	containerdRPM, err := findLatestRPMArch(dockerRPMPrimary, "containerd.io", rpmArch)
	if err != nil {
		return upstreamArch{}, "", err
	}
	containerdRPMSource := dockerRPMBase + "/" + containerdRPM.Location.Href
	sums.record(containerdRPMSource, containerdRPM.Checksum)
	a.RPM = append(a.RPM, upstreamPackageFile{
		Package: "containerd.io",
		Source:  containerdRPMSource,
		Target:  "/var/lib/kubernetes/rpm/" + path.Base(containerdRPM.Location.Href),
		SHA256:  containerdRPM.Checksum,
		Arch:    arch,
	})

	return a, kubeletVersion, nil
}

// newUpstreamVersion builds one tag's upstreamVersion: every architecture's package files, the
// image versions kubeadm's own source pins, and the Kubernetes version they all render against.
func newUpstreamVersion(tag string, cache *upstreamRepoCache, sums *exampleShasums) (upstreamVersion, error) {
	semver, err := tagVersion(tag)
	if err != nil {
		return upstreamVersion{}, err
	}
	minor := fmt.Sprintf("%d.%d", semver[0], semver[1])
	target := fmt.Sprintf("%d.%d.%d", semver[0], semver[1], semver[2])

	v := upstreamVersion{Minor: minor}
	for _, arch := range exampleMultiArches {
		rpmArch, ok := exampleRPMArches[arch]
		if !ok {
			return upstreamVersion{}, fmt.Errorf("no rpm architecture known for %s", arch)
		}
		a, kubeletVersion, err := fetchUpstreamArch(minor, target, arch, rpmArch, cache, sums)
		if err != nil {
			return upstreamVersion{}, err
		}
		v.Arches = append(v.Arches, a)
		if v.Version == "" {
			v.Version = kubeletVersion
		}
	}
	v.MultiArch = len(v.Arches) > 1

	if v.EtcdVersion, v.CoreDNSVersion, v.PauseVersion, err = kubeadmConstants(tag); err != nil {
		return upstreamVersion{}, err
	}
	return v, nil
}

// parseUpstreamTemplate parses upstream-distro.yaml.tmpl.
func parseUpstreamTemplate() (*template.Template, error) {
	tmpl, err := template.New(filepath.Base(upstreamTemplate)).ParseFiles(upstreamTemplate)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", upstreamTemplate, err)
	}
	return tmpl, nil
}

// renderOneUpstreamExample fetches and writes one tag's example/upstream/<minor>/<version>/
// distro.yaml, and reports the path it wrote.
func renderOneUpstreamExample(tmpl *template.Template, tag string, cache *upstreamRepoCache, sums *exampleShasums) (string, error) {
	v, err := newUpstreamVersion(tag, cache, sums)
	if err != nil {
		return "", fmt.Errorf("generating upstream example for %s: %w", tag, err)
	}

	minor, err := tagMinor(tag)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(upstreamDir, minor, "v"+v.Version)

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, v); err != nil {
		return "", fmt.Errorf("rendering upstream example for %s: %w", tag, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "distro.yaml")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// renderUpstreamExamples renders example/upstream/<minor>/<version>/distro.yaml for every tag
// pins.json pins for upstream, plus whatever is already on disk. There is no probe hook: unlike
// Rancher's RPM revision pruning, pkgs.k8s.io keeps every published minor's packages available,
// so a fetch failure means something worth investigating rather than a routine retirement.
func renderUpstreamExamples(pinned []string, sums *exampleShasums) error {
	spec := exampleDistroSpec{name: "upstream"}
	f := exampleFlavor{dir: upstreamDir}

	tags, err := exampleTags(pinned, spec, f)
	if err != nil {
		return err
	}

	tmpl, err := parseUpstreamTemplate()
	if err != nil {
		return err
	}

	cache := newUpstreamRepoCache()
	for _, tag := range tags {
		path, err := renderOneUpstreamExample(tmpl, tag, cache, sums)
		if err != nil {
			return err
		}
		fmt.Println("Generated " + path)
	}
	return nil
}

// stableTags drops pre-release tags. remoteTags already excludes "rc" tags, but
// kubernetes/kubernetes also publishes "alpha"/"beta" pre-releases that k3s/rke2 never do, and
// pins.json only ever pins the stable release those precede, so a release line backfill has to
// filter them out itself.
func stableTags(tags []string) []string {
	var out []string
	for _, tag := range tags {
		if !strings.Contains(tag, "-") {
			out = append(out, tag)
		}
	}
	return out
}

// renderUpstreamLine backfills every stable upstream release on one minor line ("v1.37"),
// mirroring Generate.ExampleLine for the distros that fit exampleDistroSpec. Touches the
// network.
func renderUpstreamLine(repoURL, prefix string, sums *exampleShasums) error {
	tags, err := remoteTags(repoURL, prefix)
	if err != nil {
		return err
	}
	tags = stableTags(tags)
	if err := sortTagsDesc(tags); err != nil {
		return err
	}

	tmpl, err := parseUpstreamTemplate()
	if err != nil {
		return err
	}

	cache := newUpstreamRepoCache()
	for _, tag := range tags {
		path, err := renderOneUpstreamExample(tmpl, tag, cache, sums)
		if err != nil {
			return err
		}
		fmt.Println("Generated " + path)
	}
	return nil
}
