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

package tunnel_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/pkg/tunnel"
	"github.com/stretchr/testify/require"
)

// TestTLSVerifiesAgainstTheNameNotTheRoute is the property the Kubernetes API read depends on.
//
// Reading zarf state through a tunnel keeps the kubeconfig's server URL untouched and only
// replaces the dialer. That only works if TLS validates against the hostname in the URL rather
// than the address actually connected to -- otherwise every certificate would need a SAN for
// whatever node-local address the tunnel opens onto, which no cluster issues.
//
// The server here holds a certificate for "control.internal" alone, with no IP SANs at all, and
// listens on loopback. A client asking for https://control.internal:6443 reaches it through the
// tunnel and verifies, which is exactly the arrangement rest.Config.Dial produces.
func TestTLSVerifiesAgainstTheNameNotTheRoute(t *testing.T) {
	const serverName = "control.internal"

	cert, pool := certificateFor(t, serverName)
	addr := tlsServer(t, cert)

	sshAddr := tunnelSSHServer(t)
	conn, err := tunnel.Open(context.Background(), sshConfigFor(t, sshAddr))
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	dialer, err := conn.To(addr)
	require.NoError(t, err)

	client := &http.Client{Transport: &http.Transport{
		DialContext:     dialer.DialContext,
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}

	// The name is never resolved and the port is not the one listened on: both come from the
	// URL, and the dialer overrides them. Verification still uses the name.
	resp, err := client.Get("https://" + serverName + ":6443/healthz") //nolint:noctx
	require.NoError(t, err, "TLS did not verify against the hostname, so a kubeconfig's server URL cannot be left alone")
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "ok", string(body))
}

// TestTLSStillRejectsTheWrongName checks the verification is real rather than disabled by the
// dialer. A tunnel that silently skipped certificate checks would be worse than no tunnel.
func TestTLSStillRejectsTheWrongName(t *testing.T) {
	cert, pool := certificateFor(t, "control.internal")
	addr := tlsServer(t, cert)

	sshAddr := tunnelSSHServer(t)
	conn, err := tunnel.Open(context.Background(), sshConfigFor(t, sshAddr))
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	dialer, err := conn.To(addr)
	require.NoError(t, err)

	client := &http.Client{Transport: &http.Transport{
		DialContext:     dialer.DialContext,
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}

	_, err = client.Get("https://impostor.internal:6443/healthz") //nolint:noctx
	require.ErrorContains(t, err, "certificate is valid for")
}

// certificateFor builds a self-signed certificate for one DNS name, with no IP SANs, so that a
// connection verified by address alone could not possibly pass.
func certificateFor(t *testing.T, dnsName string) (tls.Certificate, *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: dnsName},
		DNSNames:              []string{dnsName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	parsed, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(parsed)

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

// tlsServer serves HTTPS on loopback with the given certificate, standing in for an API server
// listening on a node.
func tlsServer(t *testing.T, cert tls.Certificate) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, "ok")
		}),
	}
	go func() {
		_ = srv.ServeTLS(ln, "", "") //nolint:errcheck
	}()
	t.Cleanup(func() { _ = srv.Close() }) //nolint:errcheck

	return ln.Addr().String()
}
