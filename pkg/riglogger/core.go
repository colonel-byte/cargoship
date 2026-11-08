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

// Package riglogger bridges cargoship's context-scoped logger into rig v2's
// slog-based logging. rig v2 removed the v0.x global rig.SetLogger(l)
// setter -- logging is now configured per client via the rig.WithLogger
// client option. Since rig clients are constructed deep inside the phase
// pipeline (via Host.Connect, see
// api/colonel-byte.dev/v1alpha1/cluster/host.go) rather than at a single call
// site with easy access to the request context, this package stores the
// most recently configured logger so that Host.Connect can pass it to
// rig.WithLogger without threading a logger argument through every phase.
package riglogger

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// rigSlogHandler adapts cargoship's context-scoped logger to an slog.Handler
// so rig v2's internal logging is routed through it.
type rigSlogHandler struct {
	ctx context.Context //nolint:containedctx // bound once at RigLogger time; rig's slog.Logger has no per-call context plumbing
	// attrs are those accumulated through WithAttrs, and group is the prefix from WithGroup.
	// rig attaches the host this way -- log.With(slog.String("host", conn.String())) -- so
	// discarding them lost the one attribute that makes a 30-host run readable.
	attrs []slog.Attr
	group string
}

// Enabled implements slog.Handler. It answers true unconditionally and leaves level filtering to
// the sink: this handler cannot see the level the context logger was configured with, and guessing
// one here would drop records the --log-file debug handler exists to keep.
func (rigSlogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

// Handle implements slog.Handler.
//
// r's attrs are forwarded as-is rather than flattened into the message: structured sinks like
// the --log-file JSON handler need the raw key-value pairs, not just a formatted string.
func (h rigSlogHandler) Handle(_ context.Context, r slog.Record) error {
	// h.ctx, not the per-call ctx -- which is why the struct holds one at all. rig builds its
	// slog.Logger once and calls it with no context plumbing, so the ctx arriving here carries no
	// logger, and logger.From answers such a context with a discard handler. Reading it meant
	// every line rig produced was thrown away: no SSH connection detail in the console output and
	// none in the --log-file debug log either.
	l := logger.From(h.ctx)
	msg := r.Message

	args := make([]any, 0, len(h.attrs)+r.NumAttrs())
	for _, a := range h.attrs {
		args = append(args, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		if h.group != "" {
			a.Key = h.group + "." + a.Key
		}
		args = append(args, a)
		return true
	})

	switch {
	case r.Level >= slog.LevelError:
		l.Error(msg, args...)
	case r.Level >= slog.LevelWarn:
		l.Warn(msg, args...)
	case r.Level >= slog.LevelInfo:
		l.Info(msg, args...)
	default:
		l.Debug(msg, args...)
	}
	return nil
}

// WithAttrs implements slog.Handler. The returned handler carries attrs in addition to those it
// already holds, copied into a fresh slice rather than appended in place so that two loggers
// derived from the same parent cannot scribble over each other's backing array.
func (h rigSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	// Prefixed with the group held right now, the same way Handle prefixes a record's own
	// attrs. Storing them bare made the two paths disagree: under WithGroup("ssh"), an attr
	// passed per call came out as ssh.user while the same attr added through With came out as
	// user. slog's contract is that an attr added after WithGroup belongs to that group.
	for _, a := range attrs {
		if h.group != "" {
			a.Key = h.group + "." + a.Key
		}
		merged = append(merged, a)
	}
	h.attrs = merged
	return h
}

// WithGroup implements slog.Handler. Groups are flattened to a dotted key prefix, because the sink
// is cargoship's own logger reached through its variadic args, which has no way to open a group.
func (h rigSlogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	if h.group != "" {
		name = h.group + "." + name
	}
	h.group = name
	return h
}

var current atomic.Pointer[slog.Logger]

// RigLogger overrides rig's logger with our custom logger, bound to ctx. The
// resulting logger is retrieved by Host.Connect (via Logger) and passed
// to rig.WithLogger when the underlying rig client is constructed.
func RigLogger(ctx context.Context) error {
	current.Store(slog.New(rigSlogHandler{ctx: ctx}))
	return nil
}

// Logger returns the currently configured rig logger. If RigLogger has not
// been called yet, it returns a logger that discards everything.
func Logger() *slog.Logger {
	if l := current.Load(); l != nil {
		return l
	}
	return slog.New(slog.DiscardHandler)
}
