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

// Package upstream renders example/upstream's distro.yaml examples.
//
// Upstream (kubeadm) does not fit the distroSpec shape magefiles/pkg/example renders the other
// distros through: it installs five independently-versioned OS packages from pkgs.k8s.io and
// containerd.io from download.docker.com, rather than one release's binaries and image list. So
// it gets its own fetch pipeline and its own package, rather than a fifth distroSpec entry.
package upstream

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/colonel-byte/cargoship/magefiles/pkg/engine/pins"
	"github.com/colonel-byte/cargoship/magefiles/pkg/example/arch"
	"github.com/colonel-byte/cargoship/magefiles/pkg/example/layout"
	"github.com/colonel-byte/cargoship/magefiles/pkg/example/shasums"
	"github.com/colonel-byte/cargoship/magefiles/pkg/rpmrepo"
)

const (
	templatePath = "magefiles/templates/upstream-distro.yaml.tmpl"
	upstreamDir  = "example/upstream"

	// upstreamDebCodename is the Debian release pkgs.k8s.io and download.docker.com both
	// publish packages for. The example targets one OS baseline, so this is a constant
	// rather than a lookup.
	upstreamDebCodename = "trixie"

	// upstreamDockerRPMRelease is the RHEL major release download.docker.com's rpm repo is
	// split by, matching the centos/9 baseline rpm.rancher.io's own examples use.
	upstreamDockerRPMRelease = "9"
)

// osPackages is every OS package the example installs, in the order the template
// renders them. containerd.io comes from download.docker.com; the rest come from pkgs.k8s.io.
var osPackages = []string{"cri-tools", "kubernetes-cni", "kubeadm", "kubelet", "kubectl"}

// packageFile is one architecture's copy of one OS package, ready for a ZarfFile entry.
type packageFile struct {
	Package string // kubeadm -- for readability in the rendered comments
	Source  string
	Target  string
	SHA256  string
	Arch    string // amd64 -- what the file's selector.arch names
}

// archFiles is one architecture's share of the example: every package file pkgs.k8s.io and
// download.docker.com publish for it, as both a .deb and an .rpm.
type archFiles struct {
	Arch    string // amd64
	RPMArch string // x86_64 -- what pkgs.k8s.io and download.docker.com call the same architecture
	Deb     []packageFile
	RPM     []packageFile
}

// version is what upstream-distro.yaml.tmpl renders against.
//
// Version is kubelet's currently published version rather than the pinned kubernetes/kubernetes
// tag: kubeadm, kubelet, and kubectl are independently versioned OS packages that do not always
// share a patch release (kubeadm trails kubelet by a patch as often as not), and kubelet's
// version is what the control-plane images below have always tracked, so deriving both from the
// same source keeps the example internally consistent instead of quietly drifting apart the way
// the hand-written file did.
type version struct {
	Version string // 1.35.3 -- spec.version, and the registry.k8s.io/kube-*:vX.Y.Z image tag
	Minor   string // 1.35 -- the pkgs.k8s.io URL segment

	EtcdVersion    string // 3.6.6-0
	CoreDNSVersion string // v1.13.1
	PauseVersion   string // 3.10.1

	Arches    []archFiles
	MultiArch bool
}

const rawGithubBase = "https://raw.githubusercontent.com"

var (
	upstreamEtcdVersionPattern    = regexp.MustCompile(`DefaultEtcdVersion\s*=\s*"([^"]+)"`)
	upstreamCoreDNSVersionPattern = regexp.MustCompile(`CoreDNSVersion\s*=\s*"([^"]+)"`)
	upstreamPauseVersionPattern   = regexp.MustCompile(`PauseVersion\s*=\s*"([^"]+)"`)
)

// kubeadmConstants is fetchKubeadmConstants against the real kubernetes/kubernetes repo.
func kubeadmConstants(tag string) (etcd, coredns, pause string, err error) {
	return fetchKubeadmConstants(rawGithubBase, tag)
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
	defer resp.Body.Close() //nolint:errcheck // read-only body, nothing to do with a close error
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
	defer resp.Body.Close() //nolint:errcheck // read-only body, nothing to do with a close error
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
		if !found || rpmrepo.CompareValues(s.Version, best.Version) > 0 {
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
		if rpmrepo.CompareValues(base, target) > 0 {
			continue
		}
		if !found || rpmrepo.CompareValues(base, debBaseVersion(best.Version)) > 0 {
			best = s
			found = true
		}
	}
	if !found {
		return debStanza{}, fmt.Errorf("no %s package for %s at or before %s in Packages index", pkg, arch, target)
	}
	return best, nil
}

// repoCache memoizes the four repodata documents a backfill needs -- k8s's deb/rpm
// indexes per minor line, and download.docker.com's deb/rpm indexes per architecture. All four
// are the same for every tag on a line (k8s's cover every patch already published, and
// containerd.io does not vary by k8s tag at all), so without this a backfill of N patches
// downloaded and reparsed each multi-megabyte index N times over.
type repoCache struct {
	k8sDeb    map[string][]debStanza
	k8sRPM    map[string]*rpmrepo.Primary
	dockerDeb map[string][]debStanza
	dockerRPM map[string]*rpmrepo.Primary
}

func newRepoCache() *repoCache {
	return &repoCache{
		k8sDeb:    map[string][]debStanza{},
		k8sRPM:    map[string]*rpmrepo.Primary{},
		dockerDeb: map[string][]debStanza{},
		dockerRPM: map[string]*rpmrepo.Primary{},
	}
}

func (c *repoCache) k8sDebStanzas(minor string) ([]debStanza, error) {
	if s, ok := c.k8sDeb[minor]; ok {
		return s, nil
	}
	s, err := fetchDebPackages(k8sDebBase(minor) + "/Packages")
	if err != nil {
		return nil, err
	}
	c.k8sDeb[minor] = s
	return s, nil
}

func (c *repoCache) k8sRPMPrimary(minor string) (*rpmrepo.Primary, error) {
	if p, ok := c.k8sRPM[minor]; ok {
		return p, nil
	}
	p, err := rpmrepo.FetchPrimary(context.Background(), http.DefaultClient, k8sRPMBase(minor))
	if err != nil {
		return nil, err
	}
	c.k8sRPM[minor] = p
	return p, nil
}

func (c *repoCache) dockerDebStanzas(arch string) ([]debStanza, error) {
	if s, ok := c.dockerDeb[arch]; ok {
		return s, nil
	}
	s, err := fetchDebPackages(dockerDebPackagesURL(arch))
	if err != nil {
		return nil, err
	}
	c.dockerDeb[arch] = s
	return s, nil
}

func (c *repoCache) dockerRPMPrimary(rpmArch string) (*rpmrepo.Primary, error) {
	if p, ok := c.dockerRPM[rpmArch]; ok {
		return p, nil
	}
	p, err := rpmrepo.FetchPrimary(context.Background(), http.DefaultClient, dockerRPMBase(rpmArch))
	if err != nil {
		return nil, err
	}
	c.dockerRPM[rpmArch] = p
	return p, nil
}

// k8sDebBase is the pkgs.k8s.io deb repo for a minor line ("1.35").
func k8sDebBase(minor string) string {
	return fmt.Sprintf("https://pkgs.k8s.io/core:/stable:/v%s/deb", minor)
}

// k8sRPMBase is the pkgs.k8s.io rpm repo for a minor line.
func k8sRPMBase(minor string) string {
	return fmt.Sprintf("https://pkgs.k8s.io/core:/stable:/v%s/rpm", minor)
}

// dockerDebPackagesURL is download.docker.com's Packages index for one architecture --
// unlike pkgs.k8s.io, it publishes one file per architecture rather than one file for all of
// them.
func dockerDebPackagesURL(arch string) string {
	return fmt.Sprintf("https://download.docker.com/linux/debian/dists/%s/stable/binary-%s/Packages", upstreamDebCodename, arch)
}

// dockerRPMBase is download.docker.com's rpm repo for one architecture.
func dockerRPMBase(rpmArch string) string {
	return fmt.Sprintf("https://download.docker.com/linux/rhel/%s/%s/stable", upstreamDockerRPMRelease, rpmArch)
}

// fetchArch builds one architecture's package file list: every pkgs.k8s.io package as
// both a .deb and an .rpm, plus containerd.io from download.docker.com in the same two forms.
// target is the tag's own version ("1.37.0"): pkgs.k8s.io keeps every patch's packages on the
// minor line, so each tag selects its own package rather than always the newest on the line.
// containerd.io has no such anchor -- its release cadence is independent of the k8s tag -- so it
// still always takes the newest published version. kubeletVersion returns kubelet's
// newly-discovered deb/rpm version, since that is what the caller derives the example's
// Kubernetes version from.
func fetchArch(minor, target, arch, rpmArch string, cache *repoCache, sums *shasums.Cache) (archFiles, string, error) {
	a := archFiles{Arch: arch, RPMArch: rpmArch}
	debBase := k8sDebBase(minor)
	rpmBase := k8sRPMBase(minor)

	debStanzas, err := cache.k8sDebStanzas(minor)
	if err != nil {
		return archFiles{}, "", err
	}
	k8sRPMPrimary, err := cache.k8sRPMPrimary(minor)
	if err != nil {
		return archFiles{}, "", err
	}

	var kubeletVersion string
	for _, pkg := range osPackages {
		deb, err := pickDebStanza(debStanzas, pkg, arch, target)
		if err != nil {
			return archFiles{}, "", err
		}
		debSource := debBase + "/" + deb.Filename
		sums.Record(debSource, deb.SHA256)
		a.Deb = append(a.Deb, packageFile{
			Package: pkg,
			Source:  debSource,
			Target:  "/var/lib/kubernetes/deb/" + path.Base(deb.Filename),
			SHA256:  deb.SHA256,
			Arch:    arch,
		})

		rpmPkg, err := rpmrepo.PickArch(k8sRPMPrimary, pkg, rpmArch, target)
		if err != nil {
			return archFiles{}, "", err
		}
		rpmSource := rpmBase + "/" + rpmPkg.Location.Href
		sums.Record(rpmSource, rpmPkg.Checksum)
		a.RPM = append(a.RPM, packageFile{
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
		return archFiles{}, "", err
	}
	containerdDeb, err := newestDebStanza(dockerDeb, "containerd.io", arch)
	if err != nil {
		return archFiles{}, "", err
	}
	containerdDebSource := "https://download.docker.com/linux/debian/" + containerdDeb.Filename
	sums.Record(containerdDebSource, containerdDeb.SHA256)
	a.Deb = append(a.Deb, packageFile{
		Package: "containerd.io",
		Source:  containerdDebSource,
		Target:  "/var/lib/kubernetes/deb/" + path.Base(containerdDeb.Filename),
		SHA256:  containerdDeb.SHA256,
		Arch:    arch,
	})

	dockerRPMBase := dockerRPMBase(rpmArch)
	dockerRPMPrimary, err := cache.dockerRPMPrimary(rpmArch)
	if err != nil {
		return archFiles{}, "", err
	}
	containerdRPM, err := rpmrepo.FindLatestArch(dockerRPMPrimary, "containerd.io", rpmArch)
	if err != nil {
		return archFiles{}, "", err
	}
	containerdRPMSource := dockerRPMBase + "/" + containerdRPM.Location.Href
	sums.Record(containerdRPMSource, containerdRPM.Checksum)
	a.RPM = append(a.RPM, packageFile{
		Package: "containerd.io",
		Source:  containerdRPMSource,
		Target:  "/var/lib/kubernetes/rpm/" + path.Base(containerdRPM.Location.Href),
		SHA256:  containerdRPM.Checksum,
		Arch:    arch,
	})

	return a, kubeletVersion, nil
}

// newVersion builds one tag's version: every architecture's package files, the
// image versions kubeadm's own source pins, and the Kubernetes version they all render against.
func newVersion(tag string, cache *repoCache, sums *shasums.Cache) (version, error) {
	semver, err := pins.TagVersion(tag)
	if err != nil {
		return version{}, err
	}
	minor := fmt.Sprintf("%d.%d", semver[0], semver[1])
	target := fmt.Sprintf("%d.%d.%d", semver[0], semver[1], semver[2])

	v := version{Minor: minor}
	for _, goArch := range arch.Multi {
		rpmArch, ok := arch.RPM[goArch]
		if !ok {
			return version{}, fmt.Errorf("no rpm architecture known for %s", goArch)
		}
		a, kubeletVersion, err := fetchArch(minor, target, goArch, rpmArch, cache, sums)
		if err != nil {
			return version{}, err
		}
		v.Arches = append(v.Arches, a)
		if v.Version == "" {
			v.Version = kubeletVersion
		}
	}
	v.MultiArch = len(v.Arches) > 1

	if v.EtcdVersion, v.CoreDNSVersion, v.PauseVersion, err = kubeadmConstants(tag); err != nil {
		return version{}, err
	}
	return v, nil
}

// parseTemplate parses upstream-distro.yaml.tmpl.
func parseTemplate() (*template.Template, error) {
	tmpl, err := template.New(filepath.Base(templatePath)).ParseFiles(templatePath)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", templatePath, err)
	}
	return tmpl, nil
}

// renderOne fetches and writes one tag's example/upstream/<minor>/<version>/
// distro.yaml, and reports the path it wrote.
func renderOne(tmpl *template.Template, tag string, cache *repoCache, sums *shasums.Cache) (string, error) {
	v, err := newVersion(tag, cache, sums)
	if err != nil {
		return "", fmt.Errorf("generating upstream example for %s: %w", tag, err)
	}

	minor, err := pins.TagMinor(tag)
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

// RenderExamples renders example/upstream/<minor>/<version>/distro.yaml for every tag
// pins.json pins for upstream, plus whatever is already on disk. There is no probe hook: unlike
// Rancher's RPM revision pruning, pkgs.k8s.io keeps every published minor's packages available,
// so a fetch failure means something worth investigating rather than a routine retirement.
func RenderExamples(pinned []string, sums *shasums.Cache) error {
	// Upstream has one flavor covering every minor line, so the tags it renders are exactly
	// what the shared layout finds -- there is nothing to filter out the way a Rancher flavor
	// that only covers some minors has.
	tags, err := layout.Tags(upstreamDir, "upstream", pinned)
	if err != nil {
		return err
	}

	tmpl, err := parseTemplate()
	if err != nil {
		return err
	}

	cache := newRepoCache()
	for _, tag := range tags {
		path, err := renderOne(tmpl, tag, cache, sums)
		if err != nil {
			return err
		}
		fmt.Println("Generated " + path)
	}
	return nil
}

// stableTags drops pre-release tags. pins.RemoteTags already excludes "rc" tags, but
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

// RenderLine backfills every stable upstream release on one minor line ("v1.37"),
// mirroring Generate.ExampleLine for the distros that fit exampleDistroSpec. Touches the
// network.
func RenderLine(repoURL, prefix string, sums *shasums.Cache) error {
	tags, err := pins.RemoteTags(repoURL, prefix)
	if err != nil {
		return err
	}
	tags = stableTags(tags)
	if err := pins.SortDesc(tags); err != nil {
		return err
	}

	tmpl, err := parseTemplate()
	if err != nil {
		return err
	}

	cache := newRepoCache()
	for _, tag := range tags {
		path, err := renderOne(tmpl, tag, cache, sums)
		if err != nil {
			return err
		}
		fmt.Println("Generated " + path)
	}
	return nil
}
