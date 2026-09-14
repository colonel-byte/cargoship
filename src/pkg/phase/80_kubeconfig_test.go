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

package phase

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func testCreds() distrocfg.AdminCredentials {
	return distrocfg.AdminCredentials{
		CertificateAuthority: []byte("ca"),
		ClientCertificate:    []byte("crt"),
		ClientKey:            []byte("key"),
	}
}

func testKubeConfigPhase() *KubeConfig {
	return &KubeConfig{ClusterID: "prod", ClusterLB: "lb.example.com"}
}

// An empty config is the case a caller wanting the value hits: what comes back has to be a
// usable kubeconfig on its own, not a diff against something on disk.
func TestKubeConfigMergeBuildsStandaloneConfig(t *testing.T) {
	config := testKubeConfigPhase().merge(clientcmdapi.NewConfig(), testCreds())

	require.Len(t, config.Clusters, 1)
	require.Equal(t, "https://lb.example.com:6443", config.Clusters["prod"].Server)
	require.Equal(t, []byte("ca"), config.Clusters["prod"].CertificateAuthorityData)

	require.Len(t, config.AuthInfos, 1)
	require.Equal(t, []byte("crt"), config.AuthInfos["prod-admin"].ClientCertificateData)
	require.Equal(t, []byte("key"), config.AuthInfos["prod-admin"].ClientKeyData)

	require.Equal(t, "prod", config.Contexts["prod"].Cluster)
	require.Equal(t, "prod-admin", config.Contexts["prod"].AuthInfo)
	require.Equal(t, "prod", config.CurrentContext)
}

// The same merge runs against the operator's own kubeconfig when the CLI writes one, so an
// unrelated cluster in it has to come out untouched.
func TestKubeConfigMergeLeavesOtherClustersAlone(t *testing.T) {
	config := clientcmdapi.NewConfig()
	config.Clusters["staging"] = &clientcmdapi.Cluster{Server: "https://staging:6443"}
	config.AuthInfos["staging-admin"] = &clientcmdapi.AuthInfo{Token: "t"}
	config.Contexts["staging"] = &clientcmdapi.Context{Cluster: "staging", AuthInfo: "staging-admin"}
	config.CurrentContext = "staging"

	config = testKubeConfigPhase().merge(config, testCreds())

	require.Equal(t, "https://staging:6443", config.Clusters["staging"].Server)
	require.Equal(t, "t", config.AuthInfos["staging-admin"].Token)
	require.Equal(t, "staging", config.Contexts["staging"].Cluster)

	require.Len(t, config.Clusters, 2)
	require.Equal(t, "prod", config.CurrentContext)
}

// Updating an existing entry has to keep the fields cargoship does not set, since the
// operator may have added them by hand.
func TestKubeConfigMergeKeepsUnownedFieldsOnExistingEntry(t *testing.T) {
	config := clientcmdapi.NewConfig()
	config.Clusters["prod"] = &clientcmdapi.Cluster{
		Server:                "https://old:6443",
		ProxyURL:              "http://proxy:3128",
		InsecureSkipTLSVerify: true,
	}

	config = testKubeConfigPhase().merge(config, testCreds())

	require.Equal(t, "http://proxy:3128", config.Clusters["prod"].ProxyURL)
	require.True(t, config.Clusters["prod"].InsecureSkipTLSVerify)
	require.Equal(t, "https://lb.example.com:6443", config.Clusters["prod"].Server)
	require.Equal(t, []byte("ca"), config.Clusters["prod"].CertificateAuthorityData)
}

func TestKubeConfigBytesErrorsBeforeRun(t *testing.T) {
	_, err := testKubeConfigPhase().Bytes()
	require.ErrorIs(t, err, ErrNoKubeConfig)
}

func TestKubeConfigBytesSerializesTheBuiltConfig(t *testing.T) {
	p := testKubeConfigPhase()
	p.built = p.merge(clientcmdapi.NewConfig(), testCreds())

	raw, err := p.Bytes()
	require.NoError(t, err)
	require.Contains(t, string(raw), "https://lb.example.com:6443")
	require.Contains(t, string(raw), "current-context: prod")
}

// An unset Path has to keep resolving the way it always has: KUBECONFIG when set, the
// default file otherwise.
func TestKubeConfigPathOptionsDefaultsToTheStandardLocation(t *testing.T) {
	access := testKubeConfigPhase().pathOptions()
	require.False(t, access.IsExplicitFile())
}

func TestKubeConfigPathOptionsNarrowsToOneFile(t *testing.T) {
	t.Setenv("KUBECONFIG", "/should/be/ignored")

	p := testKubeConfigPhase()
	p.Path = "/tmp/somewhere/admin.conf"
	access := p.pathOptions()

	require.True(t, access.IsExplicitFile())
	require.Equal(t, "/tmp/somewhere/admin.conf", access.GetExplicitFile())
	require.Equal(t, "/tmp/somewhere/admin.conf", access.GetDefaultFilename())
}

// A path that is not there yet is the ordinary case for --kubeconfig: the file, and the
// directory holding it, get created.
func TestKubeConfigWriteCreatesAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "admin.conf")

	p := testKubeConfigPhase()
	p.Path = path
	p.configAccess = p.pathOptions()
	require.NoError(t, p.writeConfig(testCreds()))

	written, err := clientcmd.LoadFromFile(path)
	require.NoError(t, err)
	require.Equal(t, "https://lb.example.com:6443", written.Clusters["prod"].Server)
	require.Equal(t, "prod", written.CurrentContext)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// Pointing at a file that already holds another cluster has to add to it, not replace it.
func TestKubeConfigWriteMergesIntoAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.conf")

	existing := clientcmdapi.NewConfig()
	existing.Clusters["staging"] = &clientcmdapi.Cluster{Server: "https://staging:6443"}
	existing.AuthInfos["staging-admin"] = &clientcmdapi.AuthInfo{Token: "t"}
	existing.Contexts["staging"] = &clientcmdapi.Context{Cluster: "staging", AuthInfo: "staging-admin"}
	existing.CurrentContext = "staging"
	require.NoError(t, clientcmd.WriteToFile(*existing, path))

	p := testKubeConfigPhase()
	p.Path = path
	p.configAccess = p.pathOptions()
	require.NoError(t, p.writeConfig(testCreds()))

	written, err := clientcmd.LoadFromFile(path)
	require.NoError(t, err)
	require.Len(t, written.Clusters, 2)
	require.Equal(t, "https://staging:6443", written.Clusters["staging"].Server)
	require.Equal(t, "https://lb.example.com:6443", written.Clusters["prod"].Server)
	require.Equal(t, "prod", written.CurrentContext)
}
