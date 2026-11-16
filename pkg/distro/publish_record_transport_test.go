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

package distro

import (
	"context"
	"crypto/subtle"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/types"
	"oras.land/oras-go/v2/registry"
)

// TestPublishRecordUsesTheTransportItWasGiven covers the credentials and the trust the record
// write travels over, which is the half of a publish that is allowed to fail quietly: a record
// is advisory, so writePublishRecord logs and returns rather than failing the command. It built
// its registry client from RemoteOptions alone for a while, dropping the username, password, CA
// bundle, client certificate and dial function the push itself had used -- so against any
// registry that wants authentication or serves a private certificate, every publish recorded
// nothing and said so only in a warning. `cargoship ls` then read the registry as empty.
//
// Asserted through the record landing rather than through a returned error, because there is no
// returned error to assert on.
func TestPublishRecordUsesTheTransportItWasGiven(t *testing.T) {
	const user, pass = "mirror-writer", "s3cret"

	t.Run("credentials reach the record write", func(t *testing.T) {
		ctx := context.Background()
		host := basicAuthRegistry(t, user, pass)
		l := mirrorLayoutVersion(t, "1.36.0-rke2r1")

		writePublishRecord(ctx, l, registry.Reference{Registry: host}, registry.Reference{
			Registry:   host,
			Repository: "rke2",
			Reference:  "1.36.0-rke2r1",
		}, PublishOptions{
			Retries:       1,
			RemoteOptions: types.RemoteOptions{PlainHTTP: true},
			Username:      user,
			Password:      pass,
		})

		opts := listOptions("rke2")
		opts.Username, opts.Password = user, pass
		found, err := ListMirrored(ctx, host, opts)
		require.NoError(t, err)
		require.Len(t, found, 1, "the record did not land: the credentials never reached the client")
	})

	t.Run("a private CA reaches the record write", func(t *testing.T) {
		ctx := context.Background()
		host, caBundle := tlsRegistry(t)
		l := mirrorLayoutVersion(t, "1.36.0-rke2r1")

		writePublishRecord(ctx, l, registry.Reference{Registry: host}, registry.Reference{
			Registry:   host,
			Repository: "rke2",
			Reference:  "1.36.0-rke2r1",
		}, PublishOptions{
			Retries:  1,
			CABundle: caBundle,
		})

		found, err := ListMirrored(ctx, host, ListMirroredOptions{
			Name:     "rke2",
			CABundle: caBundle,
		})
		require.NoError(t, err)
		require.Len(t, found, 1, "the record did not land: the CA bundle never reached the client")
	})
}

// basicAuthRegistry starts an in-memory plain-HTTP registry that refuses any request not carrying
// the given basic-auth credentials, and returns its address. test.SetupInMemoryRegistry is the
// unauthenticated equivalent; it wraps nothing, so a test needing a credential builds its own.
func basicAuthRegistry(t *testing.T, user, pass string) string {
	t.Helper()
	srv := httptest.NewServer(requireBasicAuth(ggcrregistry.New(), user, pass))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

// tlsRegistry starts an in-memory registry behind TLS with a certificate no system root covers,
// and returns its address alongside the PEM bundle that validates it.
func tlsRegistry(t *testing.T) (string, []byte) {
	t.Helper()
	srv := httptest.NewTLSServer(ggcrregistry.New())
	t.Cleanup(srv.Close)
	caBundle := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	require.NotEmpty(t, caBundle)
	return strings.TrimPrefix(srv.URL, "https://"), caBundle
}

func requireBasicAuth(next http.Handler, user, pass string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, ok := r.BasicAuth()
		// Constant time, because the registry this stands in for is reached over a network.
		authed := ok &&
			subtle.ConstantTimeCompare([]byte(gotUser), []byte(user)) == 1 &&
			subtle.ConstantTimeCompare([]byte(gotPass), []byte(pass)) == 1
		if !authed {
			w.Header().Set("WWW-Authenticate", `Basic realm="cargoship-test"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
