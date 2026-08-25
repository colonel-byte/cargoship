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

// Package logging provides an slog.Handler that fans a record out to several handlers, each
// with its own independent level filter. It exists so cargoship can write every log level to a
// file for later debugging while the terminal keeps showing only what the user's configured
// --log-level asks for.
package logging

import (
	"context"
	"errors"
	"log/slog"
)

// MultiHandler dispatches each log record to every handler that wants it. Unlike wrapping a
// single slog.Logger, each handler keeps its own Enabled check: one handler can be silent at
// Info while another still records everything down to Debug.
type MultiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler returns a MultiHandler that fans records out to handlers.
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	return &MultiHandler{handlers: handlers}
}

// Enabled reports whether at least one handler wants records at level. This must stay a
// logical OR: the top-level slog.Logger calls Enabled before Handle, so if this returned false
// whenever any single handler would refuse the level, that handler would silence every other
// handler in the group along with it -- including a file handler meant to catch everything.
func (m *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle passes r to every handler whose own Enabled check accepts r.Level.
func (m *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range m.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		// Record.Clone is required here: slog.Record's Attrs iterator can only be walked
		// once safely, and each handler in the group walks it independently.
		if err := h.Handle(ctx, r.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// WithAttrs returns a MultiHandler whose children each have attrs added.
func (m *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		next[i] = h.WithAttrs(attrs)
	}
	return &MultiHandler{handlers: next}
}

// WithGroup returns a MultiHandler whose children each have the group added.
func (m *MultiHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		next[i] = h.WithGroup(name)
	}
	return &MultiHandler{handlers: next}
}
