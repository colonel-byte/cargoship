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

	"github.com/colonel-byte/cargoship/magefiles/pkg/devtools/dnfpins"
)

// upstreamRepoCache memoizes the four repodata documents a backfill needs -- k8s's deb/rpm
// indexes per minor line, and download.docker.com's deb/rpm indexes per architecture. All four
// are the same for every tag on a line (k8s's cover every patch already published, and
// containerd.io does not vary by k8s tag at all), so without this a backfill of N patches
// downloaded and reparsed each multi-megabyte index N times over.
type upstreamRepoCache struct {
	k8sDeb    map[string][]debStanza
	k8sRPM    map[string]*dnfpins.PrimaryXML
	dockerDeb map[string][]debStanza
	dockerRPM map[string]*dnfpins.PrimaryXML
}

func newUpstreamRepoCache() *upstreamRepoCache {
	return &upstreamRepoCache{
		k8sDeb:    map[string][]debStanza{},
		k8sRPM:    map[string]*dnfpins.PrimaryXML{},
		dockerDeb: map[string][]debStanza{},
		dockerRPM: map[string]*dnfpins.PrimaryXML{},
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

func (c *upstreamRepoCache) k8sRPMPrimary(minor string) (*dnfpins.PrimaryXML, error) {
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

func (c *upstreamRepoCache) dockerRPMPrimary(rpmArch string) (*dnfpins.PrimaryXML, error) {
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
