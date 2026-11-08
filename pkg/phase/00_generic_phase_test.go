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
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

func TestVersionSkewTooGreat(t *testing.T) {
	for _, tt := range []struct {
		name    string
		running string
		target  string
		want    bool
	}{
		{name: "same minor", running: "v1.35.0", target: "v1.35.3", want: false},
		{name: "one minor ahead", running: "v1.34.0", target: "v1.35.0", want: false},
		{name: "two minors ahead", running: "v1.33.0", target: "v1.35.0", want: true},
		{name: "major version change", running: "v1.35.0", target: "v2.0.0", want: true},
		{name: "unparseable running version", running: "not-a-version", target: "v1.35.0", want: false},
		{name: "unparseable target version", running: "v1.35.0", target: "not-a-version", want: false},
		{name: "fresh install has no version yet", running: UnknownVersion, target: "v1.37.0", want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := &GenericPhase{}
			h := &cluster.Host{}
			h.Metadata.DistroVersion = tt.running

			require.Equal(t, tt.want, p.VersionSkewTooGreat(h, tt.target))
		})
	}
}

// capturingLogger returns a context carrying a logger that writes JSON into buf, so a test can
// assert on what tickerHelper actually said rather than on its internals.
func capturingLogger(t *testing.T, buf *bytes.Buffer) context.Context {
	t.Helper()
	l := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return logger.WithContext(context.Background(), l)
}

// TestTickerHelperZeroTimeoutIsNoDeadline is the regression. reset and kube-config build their
// Manager without SetTimeout, so Timeout is zero -- and context.WithTimeout(ctx, 0) yields a
// context whose deadline has already passed. The keepalive therefore ended before its first tick
// and reported the phase as over the moment it began, which a real reset walk showed as seven
// "timed out ... timeout=0s" lines for work that was only just starting.
func TestTickerHelperZeroTimeoutIsNoDeadline(t *testing.T) {
	var buf bytes.Buffer
	ctx := capturingLogger(t, &buf)
	p := &GenericPhase{manager: &Manager{}}

	cancel, err := p.tickerHelper(ctx, "stopping engines", time.Hour)
	require.NoError(t, err)

	// Long enough that an already-expired context would have fired by now.
	time.Sleep(50 * time.Millisecond)
	require.NotContains(t, buf.String(), "timed out",
		"a zero Timeout must mean no deadline, not one already passed")
	require.Empty(t, buf.String(), "nothing should be reported while the work is still running")

	cancel()
	require.Eventually(t, func() bool {
		return strings.Contains(buf.String(), "finished: stopping engines")
	}, time.Second, 10*time.Millisecond)
	require.NotContains(t, buf.String(), "timed out")
}

// TestTickerHelperReportsATimeout keeps the other half honest: a Manager that does set a timeout
// must still say so once the deadline passes.
func TestTickerHelperReportsATimeout(t *testing.T) {
	var buf bytes.Buffer
	ctx := capturingLogger(t, &buf)
	p := &GenericPhase{manager: &Manager{Timeout: 20 * time.Millisecond}}

	cancel, err := p.tickerHelper(ctx, "uploading files", time.Hour)
	require.NoError(t, err)
	defer cancel()

	require.Eventually(t, func() bool {
		return strings.Contains(buf.String(), "timed out: uploading files")
	}, time.Second, 10*time.Millisecond)
}

// TestTickerHelperDoesNotClaimCompletion pins the wording. The goroutine cannot know whether the
// work succeeded -- the caller defers cancel either way -- so it reports that it stopped watching,
// not that the task completed.
func TestTickerHelperDoesNotClaimCompletion(t *testing.T) {
	var buf bytes.Buffer
	ctx := capturingLogger(t, &buf)
	p := &GenericPhase{manager: &Manager{}}

	cancel, err := p.tickerHelper(ctx, "deleting nodes", time.Hour)
	require.NoError(t, err)
	cancel()

	require.Eventually(t, func() bool {
		return strings.Contains(buf.String(), "finished: deleting nodes")
	}, time.Second, 10*time.Millisecond)
	require.NotContains(t, buf.String(), "completed task")
}
