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
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/stretchr/testify/require"
)

func TestCaptureServiceLogsOnFailureNoOpWhenWaitSucceeded(t *testing.T) {
	config.CommonOptions.CachePath = t.TempDir()

	p := &GenericPhase{}
	h := &cluster.ZarfHost{}

	err := p.captureServiceLogsOnFailure(context.Background(), h, "k3s", time.Now(), nil)

	require.NoError(t, err)
	require.NoDirExists(t, filepath.Join(config.CommonOptions.CachePath, "logs"))
}

func TestWriteServiceLogFileSanitizesNameAndWritesContent(t *testing.T) {
	config.CommonOptions.CachePath = t.TempDir()
	h := &cluster.ZarfHost{}

	path, err := writeServiceLogFile(h, "k3s agent", "journal output here")
	require.NoError(t, err)

	require.FileExists(t, path)
	require.Equal(t, filepath.Join(config.CommonOptions.CachePath, "logs"), filepath.Dir(path))

	// service name with a space must not survive into the filename unsanitized
	require.Contains(t, filepath.Base(path), "k3s_agent")
	require.NotContains(t, filepath.Base(path), " ")

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "journal output here", string(content))
}

func TestWriteServiceLogFileDoesNotClobberConcurrentCalls(t *testing.T) {
	config.CommonOptions.CachePath = t.TempDir()
	h := &cluster.ZarfHost{}

	pathA, err := writeServiceLogFile(h, "k3s", "first attempt")
	require.NoError(t, err)
	time.Sleep(time.Second) // filenames are second-resolution timestamps
	pathB, err := writeServiceLogFile(h, "k3s", "second attempt")
	require.NoError(t, err)

	require.NotEqual(t, pathA, pathB)

	first, err := os.ReadFile(pathA)
	require.NoError(t, err)
	require.Equal(t, "first attempt", string(first))

	second, err := os.ReadFile(pathB)
	require.NoError(t, err)
	require.Equal(t, "second attempt", string(second))
}

func TestCaptureServiceLogsOnFailureReturnsOriginalErrorEvenWhenCaptureFails(t *testing.T) {
	// An unresolved host (no connection configured) makes h.ExecOutput fail; the capture
	// helper must still hand back the real wait error rather than swallowing or replacing it.
	config.CommonOptions.CachePath = t.TempDir()

	p := &GenericPhase{}
	h := &cluster.ZarfHost{}
	waitErr := errors.New("service k3s is not running")

	err := p.captureServiceLogsOnFailure(context.Background(), h, "k3s", time.Now(), waitErr)

	require.ErrorIs(t, err, waitErr)
}
