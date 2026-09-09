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
	"errors"
	"fmt"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"k8s.io/client-go/tools/clientcmd"
)

// ErrNoAdminCredentials if the admin kubeconfig on the host does not carry a usable
// CA certificate and admin client key pair
var ErrNoAdminCredentials = errors.New("admin kubeconfig has no admin credentials")

// AdminCredentials is the CA certificate and admin client key pair an engine writes on
// a controller host, in PEM form.
type AdminCredentials struct {
	// CertificateAuthority is the CA certificate the API server's serving cert is signed with
	CertificateAuthority []byte
	// ClientCertificate is the admin client certificate
	ClientCertificate []byte
	// ClientKey is the key for ClientCertificate
	ClientKey []byte
}

// adminCredentials reads the admin kubeconfig at path off host and pulls the CA certificate
// and admin client key pair out of it. Every distro keeps that material somewhere different
// -- rke2 and k3s each under their own data dir, upstream under /etc/kubernetes -- but each
// one writes an admin kubeconfig with all three in it, so going through the kubeconfig keeps
// this to one code path rather than a per-distro set of certificate paths.
func adminCredentials(host *cluster.ZarfHost, path string) (AdminCredentials, error) {
	raw, err := host.ReadFile(path)
	if err != nil {
		return AdminCredentials{}, fmt.Errorf("failed to read admin kubeconfig %s: %w", path, err)
	}

	cfg, err := clientcmd.Load([]byte(raw))
	if err != nil {
		return AdminCredentials{}, fmt.Errorf("failed to parse admin kubeconfig %s: %w", path, err)
	}

	kubeCtx, ok := cfg.Contexts[cfg.CurrentContext]
	if !ok {
		return AdminCredentials{}, fmt.Errorf("%w: %s has no context %q", ErrNoAdminCredentials, path, cfg.CurrentContext)
	}

	clu, ok := cfg.Clusters[kubeCtx.Cluster]
	if !ok {
		return AdminCredentials{}, fmt.Errorf("%w: %s has no cluster %q", ErrNoAdminCredentials, path, kubeCtx.Cluster)
	}

	auth, ok := cfg.AuthInfos[kubeCtx.AuthInfo]
	if !ok {
		return AdminCredentials{}, fmt.Errorf("%w: %s has no user %q", ErrNoAdminCredentials, path, kubeCtx.AuthInfo)
	}

	creds := AdminCredentials{}

	// Distros embed the material in the kubeconfig, but it is just as valid for a
	// kubeconfig to point at files next to it, so follow those off the host too.
	if creds.CertificateAuthority, err = resolvePEM(host, clu.CertificateAuthorityData, clu.CertificateAuthority); err != nil {
		return AdminCredentials{}, err
	}
	if creds.ClientCertificate, err = resolvePEM(host, auth.ClientCertificateData, auth.ClientCertificate); err != nil {
		return AdminCredentials{}, err
	}
	if creds.ClientKey, err = resolvePEM(host, auth.ClientKeyData, auth.ClientKey); err != nil {
		return AdminCredentials{}, err
	}

	switch {
	case len(creds.CertificateAuthority) == 0:
		return AdminCredentials{}, fmt.Errorf("%w: %s has no certificate authority", ErrNoAdminCredentials, path)
	case len(creds.ClientCertificate) == 0:
		return AdminCredentials{}, fmt.Errorf("%w: %s has no client certificate", ErrNoAdminCredentials, path)
	case len(creds.ClientKey) == 0:
		return AdminCredentials{}, fmt.Errorf("%w: %s has no client key", ErrNoAdminCredentials, path)
	}

	return creds, nil
}

// resolvePEM returns data when the kubeconfig embedded it, and otherwise reads the file
// the kubeconfig referenced off the host.
func resolvePEM(host *cluster.ZarfHost, data []byte, path string) ([]byte, error) {
	if len(data) > 0 {
		return data, nil
	}
	if path == "" {
		return nil, nil
	}

	raw, err := host.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s referenced by the admin kubeconfig: %w", path, err)
	}
	return []byte(raw), nil
}
