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
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

const (
	testCA  = "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----\n"
	testCrt = "-----BEGIN CERTIFICATE-----\ncrt\n-----END CERTIFICATE-----\n"
	testKey = "-----BEGIN EC PRIVATE KEY-----\nkey\n-----END EC PRIVATE KEY-----\n"
)

// embeddedKubeconfig is the shape both rke2 and k3s write: one context, everything
// base64 embedded, pointed at the local API server.
func embeddedKubeconfig(ca, crt, key string) string {
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: default
  cluster:
    server: https://127.0.0.1:6443
    certificate-authority-data: %s
users:
- name: default
  user:
    client-certificate-data: %s
    client-key-data: %s
contexts:
- name: default
  context:
    cluster: default
    user: default
current-context: default
`, b64(ca), b64(crt), b64(key))
}

func TestAdminCredentialsK3SReadsItsOwnKubeconfig(t *testing.T) {
	d := &K3S{RancherCommon{Common{Config: "/etc/rancher/k3s/config.yaml", Data: "/var/lib/rancher/k3s"}}}
	cfg := &fakeConfigurer{files: map[string]string{
		"/etc/rancher/k3s/k3s.yaml": embeddedKubeconfig(testCA, testCrt, testKey),
	}}
	host := cluster.ZarfHost{Configurer: cfg}

	creds, err := d.AdminCredentials(host, d.DataDirPath())
	require.NoError(t, err)

	require.Equal(t, testCA, string(creds.CertificateAuthority))
	require.Equal(t, testCrt, string(creds.ClientCertificate))
	require.Equal(t, testKey, string(creds.ClientKey))
}

func TestAdminCredentialsRKE2ReadsItsOwnKubeconfig(t *testing.T) {
	d := &RKE2{RancherCommon{Common{Config: "/etc/rancher/rke2/config.yaml", Data: "/var/lib/rancher/rke2"}}}
	cfg := &fakeConfigurer{files: map[string]string{
		"/etc/rancher/rke2/rke2.yaml": embeddedKubeconfig(testCA, testCrt, testKey),
	}}
	host := cluster.ZarfHost{Configurer: cfg}

	creds, err := d.AdminCredentials(host, d.DataDirPath())
	require.NoError(t, err)

	require.Equal(t, testCA, string(creds.CertificateAuthority))
	require.Equal(t, testCrt, string(creds.ClientCertificate))
	require.Equal(t, testKey, string(creds.ClientKey))
}

func TestAdminCredentialsFollowsFileReferences(t *testing.T) {
	kubeconfig := `apiVersion: v1
kind: Config
clusters:
- name: kubernetes
  cluster:
    server: https://127.0.0.1:6443
    certificate-authority: /etc/kubernetes/pki/ca.crt
users:
- name: admin
  user:
    client-certificate: /etc/kubernetes/pki/admin.crt
    client-key: /etc/kubernetes/pki/admin.key
contexts:
- name: admin@kubernetes
  context:
    cluster: kubernetes
    user: admin
current-context: admin@kubernetes
`
	cfg := &fakeConfigurer{files: map[string]string{
		"/etc/rancher/k3s/k3s.yaml":     kubeconfig,
		"/etc/kubernetes/pki/ca.crt":    testCA,
		"/etc/kubernetes/pki/admin.crt": testCrt,
		"/etc/kubernetes/pki/admin.key": testKey,
	}}
	host := cluster.ZarfHost{Configurer: cfg}

	creds, err := adminCredentials(host, "/etc/rancher/k3s/k3s.yaml")
	require.NoError(t, err)

	require.Equal(t, testCA, string(creds.CertificateAuthority))
	require.Equal(t, testCrt, string(creds.ClientCertificate))
	require.Equal(t, testKey, string(creds.ClientKey))
}

func TestAdminCredentialsErrorsWhenMaterialIsMissing(t *testing.T) {
	tests := map[string]struct {
		kubeconfig string
	}{
		"no credentials at all": {kubeconfig: embeddedKubeconfig("", "", "")},
		"no ca":                 {kubeconfig: embeddedKubeconfig("", testCrt, testKey)},
		"no client cert":        {kubeconfig: embeddedKubeconfig(testCA, "", testKey)},
		"no client key":         {kubeconfig: embeddedKubeconfig(testCA, testCrt, "")},
		"unknown context": {kubeconfig: `apiVersion: v1
kind: Config
current-context: nope
`},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := &fakeConfigurer{files: map[string]string{"/etc/rancher/k3s/k3s.yaml": tt.kubeconfig}}
			host := cluster.ZarfHost{Configurer: cfg}

			_, err := adminCredentials(host, "/etc/rancher/k3s/k3s.yaml")
			require.ErrorIs(t, err, ErrNoAdminCredentials)
		})
	}
}

func TestAdminCredentialsErrorsOnUnparsableKubeconfig(t *testing.T) {
	cfg := &fakeConfigurer{files: map[string]string{"/etc/rancher/k3s/k3s.yaml": "\tnot: [valid"}}
	host := cluster.ZarfHost{Configurer: cfg}

	_, err := adminCredentials(host, "/etc/rancher/k3s/k3s.yaml")
	require.ErrorContains(t, err, "failed to parse admin kubeconfig")
}
