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
// pipeline (via ZarfHost.Connect, see
// src/api/zarf.dev/v1alpha1/cluster/host.go) rather than at a single call
// site with easy access to the request context, this package stores the
// most recently configured logger so that ZarfHost.Connect can pass it to
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
}

// Enabled implements slog.Handler.
func (rigSlogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

// Handle implements slog.Handler.
//
// r's attrs are forwarded as-is rather than flattened into the message: structured sinks like
// the --log-file JSON handler need the raw key-value pairs, not just a formatted string.
func (h rigSlogHandler) Handle(ctx context.Context, r slog.Record) error {
	l := logger.From(ctx)
	msg := r.Message

	args := make([]any, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
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

// WithAttrs implements slog.Handler.
func (h rigSlogHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

// WithGroup implements slog.Handler.
func (h rigSlogHandler) WithGroup(_ string) slog.Handler {
	return h
}

var current atomic.Pointer[slog.Logger]

// RigLogger overrides rig's logger with our custom logger, bound to ctx. The
// resulting logger is retrieved by ZarfHost.Connect (via Logger) and passed
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
