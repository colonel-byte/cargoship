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
// it gets its own fetch pipeline (upstream_kubeadm.go, upstream_deb.go, upstream_rpm.go,
// upstream_repo.go) and its own call from Generate.Examples rather than a fifth
// exampleDistroSpec entry.

package examples

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/engineconfig"
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
	semver, err := engineconfig.TagVersion(tag)
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

	minor, err := engineconfig.TagMinor(tag)
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
	tags, err := engineconfig.RemoteTags(repoURL, prefix)
	if err != nil {
		return err
	}
	tags = stableTags(tags)
	if err := sortTagsDesc(tags); err != nil {
		return err
	}
	tags, err = aboveFloor(tags)
	if err != nil {
		return err
	}
	if len(tags) == 0 {
		return fmt.Errorf("no upstream releases on %s at or above the %s floor", prefix, exampleMinorFloor)
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
