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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/colonel-byte/cargoship/internal/heartbeat"
)

func TestProgressSink(t *testing.T) {
	t.Run("a sink starts empty and holds the latest phase", func(t *testing.T) {
		ctx, sink := WithProgressSink(context.Background())

		require.Equal(t, Progress{}, sink.Current(), "a run that has entered no phase has reported none")

		reportProgress(ctx, "Connect to hosts", 1, 31, StateRunning, nil)
		require.Equal(t, Progress{
			Phase: "Connect to hosts",
			Index: 1,
			Total: 31,
			State: StateRunning,
		}, sink.Current())

		reportProgress(ctx, "Connect to hosts", 1, 31, StateCompleted, nil)
		reportProgress(ctx, "Upload files to hosts", 12, 31, StateRunning, nil)
		require.Equal(t, Progress{
			Phase: "Upload files to hosts",
			Index: 12,
			Total: 31,
			State: StateRunning,
		}, sink.Current())
	})

	t.Run("a failure carries the error that ended the run", func(t *testing.T) {
		ctx, sink := WithProgressSink(context.Background())
		want := errors.New("no route to host")

		reportProgress(ctx, "Upgrade Controller", 27, 31, StateFailed, want)

		got := sink.Current()
		require.Equal(t, StateFailed, got.State)
		require.ErrorIs(t, got.Err, want)
	})

	t.Run("no sink on the context is the ordinary case, not an error", func(t *testing.T) {
		require.NotPanics(t, func() {
			reportProgress(context.Background(), "Connect to hosts", 1, 31, StateRunning, nil)
		})
	})

	t.Run("one sink per context, so two runs in one process do not share an answer", func(t *testing.T) {
		first, one := WithProgressSink(context.Background())
		second, two := WithProgressSink(context.Background())

		reportProgress(first, "Upload files to hosts", 12, 31, StateRunning, nil)
		reportProgress(second, "Upgrade Worker", 28, 31, StateRunning, nil)

		require.Equal(t, "Upload files to hosts", one.Current().Phase)
		require.Equal(t, "Upgrade Worker", two.Current().Phase)
	})

	t.Run("reading while a run reports is safe", func(t *testing.T) {
		ctx, sink := WithProgressSink(context.Background())

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 1; i <= 50; i++ {
				reportProgress(ctx, "Upload files to hosts", i, 50, StateRunning, nil)
			}
		}()
		go func() {
			defer wg.Done()
			for range 50 {
				_ = sink.Current()
			}
		}()
		wg.Wait()

		require.Equal(t, 50, sink.Current().Index)
	})
}

// The heartbeat file is what the Ansible action plugin polls -- see
// docs/guides/ansible-module.md -- so it is written whether or not anything attached a sink, and
// with the same states it always carried. A reader outside this repository parses those strings.
func TestProgressStillWritesTheHeartbeatFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	heartbeat.Init(path)
	t.Cleanup(func() { heartbeat.Init("") })

	ctx, sink := WithProgressSink(context.Background())
	reportProgress(ctx, "Upload files to hosts", 12, 31, StateRunning, nil)

	b, err := os.ReadFile(path)
	require.NoError(t, err)

	var status struct {
		Phase  string `json:"phase"`
		Index  int    `json:"index"`
		Total  int    `json:"total"`
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(b, &status))
	require.Equal(t, "Upload files to hosts", status.Phase)
	require.Equal(t, 12, status.Index)
	require.Equal(t, 31, status.Total)
	require.Equal(t, "running", status.Status, "the Ansible plugin parses this string")

	// Both watchers, from the one call.
	require.Equal(t, StateRunning, sink.Current().State)
}
