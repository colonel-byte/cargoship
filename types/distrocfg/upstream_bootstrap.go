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

package distrocfg

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
)

const (
	// upstreamKubeletConfPath is the file kubeadm writes on a node once it has either formed
	// (init) or joined (join) the cluster, and only then -- unlike kubelet's own binary, which is
	// present as soon as the package is installed. It is the one honest "already bootstrapped"
	// signal upstream has; RunningVersion succeeding is not, since it only checks the binary.
	upstreamKubeletConfPath = "/etc/kubernetes/kubelet.conf"

	// upstreamJoinTokenFile and upstreamCACertHashFile are cargoship's own hand-off files, not
	// kubeadm's -- kubeadm has no static pre-shared token to point JoinTokenPath at, so the real
	// token/hash kubeadm mints during `kubeadm init` is captured here for a joining host to read
	// off the leader afterward. Both live under the leader's /etc/kubernetes tree, so
	// Upstream.CleanupPaths() already removes them on uninstall.
	upstreamJoinTokenFile  = "/etc/kubernetes/pki/cargoship-join-token"
	upstreamCACertHashFile = "/etc/kubernetes/pki/cargoship-ca-cert-hash"
)

// joinTokenRegex and caCertHashRegex extract the token and CA cert hash from `kubeadm init`'s own
// stdout, which ends with a ready-to-use `kubeadm join ... --token ... --discovery-token-ca-cert-hash
// ...` line -- reusing kubeadm's own values rather than minting a second token nobody asked for.
var (
	joinTokenRegex  = regexp.MustCompile(`--token\s+(\S+)`)
	caCertHashRegex = regexp.MustCompile(`--discovery-token-ca-cert-hash\s+(\S+)`)
)

// IsBootstrapped reports whether host has already run `kubeadm init` or `kubeadm join`.
func (d *Upstream) IsBootstrapped(host *cluster.ZarfHost) bool {
	return host.FileExist(upstreamKubeletConfPath)
}

// Bootstrap forms the cluster on the leader with `kubeadm init`, or joins it on every other host
// with `kubeadm join`, against the kubeadm-config.yaml ConfigureEngine already wrote to host.
func (d *Upstream) Bootstrap(_ context.Context, host *cluster.ZarfHost, run cluster.ZarfRuntimeMeta, _ distro.ZarfDistro) error {
	if host.Metadata.IsLeader {
		return d.bootstrapLeader(host)
	}
	return d.bootstrapJoin(host, run)
}

// bootstrapLeader runs `kubeadm init` and captures the join token and CA cert hash it mints, so a
// joining host can fetch them back off this host afterward.
func (d *Upstream) bootstrapLeader(host *cluster.ZarfHost) error {
	out, err := host.SudoExecOutput(fmt.Sprintf("kubeadm init --config %s", kubeadmConfigPath))
	if err != nil {
		return fmt.Errorf("kubeadm init: %w", err)
	}

	token, hash, err := parseJoinCommand(out)
	if err != nil {
		return err
	}

	if err := host.WriteFile(upstreamJoinTokenFile, token, "0600"); err != nil {
		return fmt.Errorf("writing join token: %w", err)
	}
	return host.WriteFile(upstreamCACertHashFile, hash, "0600")
}

// bootstrapJoin fetches the real join token and CA cert hash off the leader, rewrites this host's
// kubeadm-config.yaml with them (buildJoinConfiguration's own rendering left caCertHashes empty --
// generating that value is this step's job, not DesiredFiles'), and runs `kubeadm join`.
func (d *Upstream) bootstrapJoin(host *cluster.ZarfHost, run cluster.ZarfRuntimeMeta) error {
	if run.Leader == nil {
		return ErrNoLeader
	}

	token, err := run.Leader.ReadFile(upstreamJoinTokenFile)
	if err != nil {
		return fmt.Errorf("reading join token from leader: %w", err)
	}
	hash, err := run.Leader.ReadFile(upstreamCACertHashFile)
	if err != nil {
		return fmt.Errorf("reading CA cert hash from leader: %w", err)
	}
	token = strings.TrimSpace(token)
	hash = strings.TrimSpace(hash)

	if host.IsController() {
		run.ControllerToken = token
	} else {
		run.AgentToken = token
	}

	jc := buildJoinConfiguration(host, run)
	jc.DigMapping("discovery", "bootstrapToken")["caCertHashes"] = []string{hash}

	b, err := marshalYAMLDocs(jc, buildKubeletConfiguration())
	if err != nil {
		return err
	}
	if err := host.WriteFile(kubeadmConfigPath, string(b), modeConfigFile); err != nil {
		return err
	}

	if _, err := host.SudoExecOutput(fmt.Sprintf("kubeadm join --config %s", kubeadmConfigPath)); err != nil {
		return fmt.Errorf("kubeadm join: %w", err)
	}
	return nil
}

// parseJoinCommand extracts the token and CA cert hash from `kubeadm init`'s stdout.
func parseJoinCommand(out string) (token, hash string, err error) {
	tm := joinTokenRegex.FindStringSubmatch(out)
	if tm == nil {
		return "", "", fmt.Errorf("no join token found in kubeadm init output: %q", out)
	}
	hm := caCertHashRegex.FindStringSubmatch(out)
	if hm == nil {
		return "", "", fmt.Errorf("no CA cert hash found in kubeadm init output: %q", out)
	}
	return tm[1], hm[1], nil
}
