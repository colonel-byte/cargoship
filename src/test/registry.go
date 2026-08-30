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

package test

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/src/config"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/stretchr/testify/require"
)

// registryLogDir is the directory, relative to the cargoship cache, that the in-memory
// registry writes its request logs into.
const registryLogDir = "e2e-logs"

// SetupInMemoryRegistry starts a plain-HTTP, in-memory OCI registry on an auto-allocated
// localhost port and returns its address (host:port). Use it against the built cargoship
// binary with the "--plain-http" flag, e.g. as the destination for "cargoship publish"/"pull"
// in e2e tests, without needing a real registry or network access. The registry is torn down
// automatically via t.Cleanup.
func SetupInMemoryRegistry(t *testing.T) string {
	t.Helper()

	logger := log.New(registryLogFile(t), "", log.LstdFlags|log.Lmicroseconds)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := &http.Server{
		Handler:  registry.New(registry.Logger(logger)),
		ErrorLog: logger,
	}
	go func() {
		//nolint:errcheck // returns http.ErrServerClosed once Shutdown is called below; nothing to do with the error
		srv.Serve(ln)
	}()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		//nolint:errcheck // best-effort shutdown during test cleanup
		srv.Shutdown(shutdownCtx)
	})

	return ln.Addr().String()
}

// registryLogFile opens the file the in-memory registry logs to. The registry writes a line
// per request, which buries the output of the test actually being run, so it goes to a file
// in the cargoship cache instead of stderr. A passing test removes its log on cleanup; a
// failing one leaves it behind and reports the path, since those requests are usually what
// explains why a publish or pull failed.
func registryLogFile(t *testing.T) *os.File {
	t.Helper()

	cachePath := config.CommonOptions.CachePath
	if cachePath == "" {
		// A test binary never loads the CLI config, so CommonOptions is zero-valued.
		cachePath = config.DefaultCachePath
	}
	cacheDir, err := config.GetAbsHomePath(cachePath)
	require.NoError(t, err)

	logDir := filepath.Join(cacheDir, registryLogDir)
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	f, err := os.CreateTemp(logDir, "registry-*.log")
	require.NoError(t, err)

	// Registered before the server's cleanup, so it runs after it: the registry has stopped
	// writing by the time the file is closed.
	t.Cleanup(func() {
		require.NoError(t, f.Close())
		if t.Failed() {
			t.Logf("in-memory registry log: %s", f.Name())
			return
		}
		require.NoError(t, os.Remove(f.Name()))
	})

	return f
}
