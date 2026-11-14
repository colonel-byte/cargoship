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
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestOrasTransportTrustsACABundle covers the transport half of reaching a registry whose
// certificate comes from a private CA, which is what a zarf-managed-mTLS deployment serves.
// Trusting its CA is the alternative to turning verification off, so the pool has to actually
// reach the TLS config rather than being accepted and dropped.
func TestOrasTransportTrustsACABundle(t *testing.T) {
	t.Parallel()

	// A self-signed certificate is a CA bundle of one for this purpose: what is asserted is
	// that it lands in RootCAs, not that it chains to anything.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Zarf Registry CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	t.Run("a bundle is added to the roots", func(t *testing.T) {
		t.Parallel()

		rt, err := orasTransport(ClientOptions{CABundle: pemBytes})
		require.NoError(t, err)
		transport, ok := rt.Base.(*http.Transport)
		require.True(t, ok, "the retry transport should wrap an *http.Transport")
		require.NotNil(t, transport.TLSClientConfig.RootCAs, "the CA bundle should reach RootCAs")
		require.False(t, transport.TLSClientConfig.InsecureSkipVerify, "trusting a CA is not the same as skipping verification")
	})

	t.Run("no bundle leaves the system roots alone", func(t *testing.T) {
		t.Parallel()

		rt, err := orasTransport(ClientOptions{})
		require.NoError(t, err)
		transport, ok := rt.Base.(*http.Transport)
		require.True(t, ok)
		require.Nil(t, transport.TLSClientConfig.RootCAs)
	})

	t.Run("a bundle holding no certificate is an error rather than silently ignored", func(t *testing.T) {
		t.Parallel()

		_, err := orasTransport(ClientOptions{CABundle: []byte("this is not a certificate")})
		require.ErrorContains(t, err, "no certificate found in the CA bundle")
	})
}
