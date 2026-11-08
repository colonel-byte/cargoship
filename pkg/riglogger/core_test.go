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

package riglogger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// newCapturingCtx returns a context carrying a logger that writes JSON into buf, so a test can
// assert on the attributes that came out the far side rather than on the handler's internals.
func newCapturingCtx(t *testing.T, buf *bytes.Buffer) context.Context {
	t.Helper()
	l := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return logger.WithContext(context.Background(), l)
}

// decodeRecord parses the single JSON record in buf and returns its fields.
//
// Tests assert on these rather than on substrings of the raw line. The line carries a timestamp,
// so a substring assertion silently depends on the clock: NotContains(out, "22") passed locally at
// 16:35 and failed in CI at 22:15, where "22" is the hour. Anything numeric -- a port, a count, a
// duration -- is exposed to that, and the failure looks like a logic bug rather than a bad test.
func decodeRecord(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	line := strings.TrimSpace(buf.String())
	require.NotEmpty(t, line, "expected a log record")
	require.NotContains(t, line, "\n", "expected exactly one log record")

	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &rec))
	return rec
}

// TestWithAttrsReachesTheSink is the reason WithAttrs exists. rig attaches the host with
// log.With(slog.String("host", ...)) rather than per call, and the handler used to return itself
// unchanged -- so in a run against thirty hosts in parallel, the lines that mattered most were the
// ones that did not say which host they came from.
func TestWithAttrsReachesTheSink(t *testing.T) {
	var buf bytes.Buffer
	ctx := newCapturingCtx(t, &buf)

	l := slog.New(rigSlogHandler{ctx: ctx}).With(slog.String("host", "node-01"))
	l.Info("connected")

	require.Equal(t, "node-01", decodeRecord(t, &buf)["host"])
}

// TestWithAttrsAccumulate covers two successive With calls, and that neither derived logger can
// corrupt the other -- the failure mode a shared backing array would produce.
func TestWithAttrsAccumulate(t *testing.T) {
	var buf bytes.Buffer
	ctx := newCapturingCtx(t, &buf)

	base := slog.New(rigSlogHandler{ctx: ctx}).With(slog.String("host", "node-01"))
	withPort := base.With(slog.Int("port", 22))
	withUser := base.With(slog.String("user", "root"))

	withPort.Info("dialing")
	rec := decodeRecord(t, &buf)
	require.Equal(t, "node-01", rec["host"])
	require.EqualValues(t, 22, rec["port"])
	require.NotContains(t, rec, "user")

	buf.Reset()
	withUser.Info("dialing")
	rec = decodeRecord(t, &buf)
	require.Equal(t, "node-01", rec["host"])
	require.Equal(t, "root", rec["user"])
	require.NotContains(t, rec, "port")
}

// TestWithGroupPrefixesKeys pins the flattening. The sink is reached through variadic args, which
// cannot open a group, so a group becomes a dotted prefix instead of being dropped.
func TestWithGroupPrefixesKeys(t *testing.T) {
	var buf bytes.Buffer
	ctx := newCapturingCtx(t, &buf)

	l := slog.New(rigSlogHandler{ctx: ctx}).WithGroup("ssh").WithGroup("auth")
	l.Info("negotiating", slog.String("method", "publickey"))

	require.Equal(t, "publickey", decodeRecord(t, &buf)["ssh.auth.method"])
}

// TestWithAttrsAndGroupNoOpOnEmpty keeps the two early returns honest.
func TestWithAttrsAndGroupNoOpOnEmpty(t *testing.T) {
	h := rigSlogHandler{ctx: context.Background()}
	require.Equal(t, h, h.WithAttrs(nil))
	require.Equal(t, h, h.WithGroup(""))
}

// TestHandleUsesTheBoundContext pins the more serious half of the fix. Handle used to read the
// logger out of its per-call context, and rig calls its logger with no context plumbing at all --
// so logger.From returned a discard handler and every line rig emitted went nowhere, in the
// console and in the --log-file debug log alike. Nothing failed; the output was simply absent.
func TestHandleUsesTheBoundContext(t *testing.T) {
	var buf bytes.Buffer
	bound := newCapturingCtx(t, &buf)

	h := rigSlogHandler{ctx: bound}
	rec := slog.NewRecord(time.Time{}, slog.LevelInfo, "ssh handshake", 0)

	// A bare context, which is what rig actually passes.
	require.NoError(t, h.Handle(context.Background(), rec))
	require.Equal(t, "ssh handshake", decodeRecord(t, &buf)["msg"])
}

// TestHandleMapsLevels keeps the level switch honest; a record logged at error must not quietly
// arrive as debug, which would hide it behind the default --log-level.
func TestHandleMapsLevels(t *testing.T) {
	tests := []struct {
		name  string
		level slog.Level
		want  string
	}{
		{
			name:  "error",
			level: slog.LevelError,
			want:  "ERROR",
		},
		{
			name:  "warn",
			level: slog.LevelWarn,
			want:  "WARN",
		},
		{
			name:  "info",
			level: slog.LevelInfo,
			want:  "INFO",
		},
		{
			name:  "debug",
			level: slog.LevelDebug,
			want:  "DEBUG",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			h := rigSlogHandler{ctx: newCapturingCtx(t, &buf)}
			rec := slog.NewRecord(time.Time{}, tt.level, "msg", 0)
			require.NoError(t, h.Handle(context.Background(), rec))
			require.Equal(t, tt.want, decodeRecord(t, &buf)["level"])
		})
	}
}

// TestWithGroupPrefixesStoredAttrs covers the path TestWithGroupPrefixesKeys does not. WithGroup
// used to prefix only a record's own per-call attrs, while WithAttrs stored its attrs bare -- so
// under WithGroup("ssh") the same attribute came out as "ssh.user" when passed per call and plain
// "user" when added through With, which is the opposite of slog's contract.
func TestWithGroupPrefixesStoredAttrs(t *testing.T) {
	var buf bytes.Buffer
	ctx := newCapturingCtx(t, &buf)

	l := slog.New(rigSlogHandler{ctx: ctx}).WithGroup("ssh").With(slog.String("user", "root"))
	l.Info("authenticating")

	require.Equal(t, "root", decodeRecord(t, &buf)["ssh.user"])
}

// TestWithAttrsBeforeGroupStaysUngrouped is the other side of the contract: an attribute added
// before any group belongs to no group, and must not pick one up afterwards.
func TestWithAttrsBeforeGroupStaysUngrouped(t *testing.T) {
	var buf bytes.Buffer
	ctx := newCapturingCtx(t, &buf)

	l := slog.New(rigSlogHandler{ctx: ctx}).With(slog.String("host", "node-01")).WithGroup("ssh")
	l.Info("connected", slog.String("user", "root"))

	rec := decodeRecord(t, &buf)
	require.Equal(t, "root", rec["ssh.user"])
	require.Equal(t, "node-01", rec["host"])
	require.NotContains(t, rec, "ssh.host")
}
