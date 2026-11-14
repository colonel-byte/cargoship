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

package images

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClientCertificateParse(t *testing.T) {
	t.Parallel()

	ca := newCA(t)
	certPEM, keyPEM := ca.issue(t, "zarf-registry-client", nil)

	tests := []struct {
		name    string
		cert    ClientCertificate
		wantErr string
	}{
		{
			name: "a pair",
			cert: ClientCertificate{
				Certificate: certPEM,
				Key:         keyPEM,
			},
		},
		{
			name: "a certificate with no key",
			cert: ClientCertificate{
				Certificate: certPEM,
			},
			wantErr: "needs the key as well as the certificate",
		},
		{
			name: "a key with no certificate",
			cert: ClientCertificate{
				Key: keyPEM,
			},
			wantErr: "needs the certificate as well as the key",
		},
		{
			name: "a pair that is not a pair",
			cert: ClientCertificate{
				Certificate: certPEM,
				Key:         []byte("-----BEGIN PRIVATE KEY-----\nnope\n-----END PRIVATE KEY-----\n"),
			},
			wantErr: "unable to read the client certificate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := tt.cert.Parse()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClientCertificateIsZero(t *testing.T) {
	t.Parallel()

	require.True(t, ClientCertificate{}.IsZero())
	require.False(t, ClientCertificate{Certificate: []byte("c")}.IsZero())
	require.False(t, ClientCertificate{Key: []byte("k")}.IsZero())
}

// TestOrasTransportPresentsAClientCertificate is the end of the plumbing: a registry that requires
// a certificate of every client gets one, and the same client without one is refused by the
// server rather than quietly connecting.
//
// Against a real TLS handshake rather than by reading the config back, because the thing worth
// knowing is that the certificate is offered and accepted, not that a field was assigned.
func TestOrasTransportPresentsAClientCertificate(t *testing.T) {
	t.Parallel()

	ca := newCA(t)
	serverCert, serverKey := ca.issue(t, "zarf-docker-registry", []net.IP{net.ParseIP("127.0.0.1")})
	pair, err := tls.X509KeyPair(serverCert, serverKey)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(ca.certPEM))

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{pair},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}
	server.StartTLS()
	// Cleanup rather than defer: the subtests below are parallel, so they run after this
	// function returns and a deferred Close would shut the server before any of them connects.
	t.Cleanup(server.Close)

	clientCert, clientKey := ca.issue(t, "zarf-registry-client", nil)

	t.Run("with a certificate the registry answers", func(t *testing.T) {
		t.Parallel()

		rt, err := orasTransport(ClientOptions{
			CABundle: ca.certPEM,
			ClientCertificate: ClientCertificate{
				Certificate: clientCert,
				Key:         clientKey,
			},
		})
		require.NoError(t, err)

		// The base transport rather than the retry wrapper around it: a handshake the server
		// closes is not worth retrying through a backoff in a test.
		resp, err := (&http.Client{Transport: rt.Base}).Get(server.URL) //nolint:noctx // no context in this request
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("without one the registry refuses the connection", func(t *testing.T) {
		t.Parallel()

		rt, err := orasTransport(ClientOptions{CABundle: ca.certPEM})
		require.NoError(t, err)

		_, err = (&http.Client{Transport: rt.Base}).Get(server.URL) //nolint:noctx // no context in this request
		require.Error(t, err, "a server requiring a client certificate should refuse a client with none")
	})

	t.Run("a half-supplied pair is an error rather than a connection with no certificate", func(t *testing.T) {
		t.Parallel()

		_, err := orasTransport(ClientOptions{
			CABundle:          ca.certPEM,
			ClientCertificate: ClientCertificate{Certificate: clientCert},
		})
		require.ErrorContains(t, err, "needs the key as well as the certificate")
	})
}

// certificateAuthority issues the certificates a test needs, both halves signed by one CA so that
// each side trusts the other.
type certificateAuthority struct {
	certPEM  []byte
	template *x509.Certificate
	key      *rsa.PrivateKey
}

func newCA(t *testing.T) certificateAuthority {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Zarf Registry CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	return certificateAuthority{
		certPEM:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		template: template,
		key:      key,
	}
}

// issue returns a PEM certificate and key for one common name, usable as either end of a
// handshake: both extended usages are set, so one helper serves the server and the client.
func (ca certificateAuthority) issue(t *testing.T, commonName string, ips []net.IP) (certPEM, keyPEM []byte) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.template, &key.PublicKey, ca.key)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}
