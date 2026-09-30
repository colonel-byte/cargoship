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
	"regexp"
)

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
	defer resp.Body.Close() //nolint:errcheck // body is read to completion, nothing to do with a close error
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
