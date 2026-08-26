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

package logging

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMultiHandlerWritesEveryLevelToFileRegardlessOfConsoleLevel(t *testing.T) {
	var console, file bytes.Buffer

	// Console only shows Warn and above, file (like the log-file handler) keeps everything.
	consoleHandler := slog.NewTextHandler(&console, &slog.HandlerOptions{Level: slog.LevelWarn})
	fileHandler := slog.NewTextHandler(&file, &slog.HandlerOptions{Level: slog.LevelDebug})

	logger := slog.New(NewMultiHandler(consoleHandler, fileHandler))

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")

	require.NotContains(t, console.String(), "debug message")
	require.NotContains(t, console.String(), "info message")
	require.Contains(t, console.String(), "warn message")

	require.Contains(t, file.String(), "debug message")
	require.Contains(t, file.String(), "info message")
	require.Contains(t, file.String(), "warn message")
}

func TestMultiHandlerEnabledIsTrueIfAnyChildWants(t *testing.T) {
	quiet := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelError})
	verbose := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelDebug})

	m := NewMultiHandler(quiet, verbose)

	require.True(t, m.Enabled(t.Context(), slog.LevelDebug))
	require.False(t, NewMultiHandler(quiet).Enabled(t.Context(), slog.LevelDebug))
}

func TestMultiHandlerWithAttrsAppliesToEveryChild(t *testing.T) {
	var a, b bytes.Buffer
	m := NewMultiHandler(
		slog.NewTextHandler(&a, &slog.HandlerOptions{Level: slog.LevelDebug}),
		slog.NewTextHandler(&b, &slog.HandlerOptions{Level: slog.LevelDebug}),
	)

	logger := slog.New(m).With("requestId", "abc123")
	logger.Info("hello")

	require.Contains(t, a.String(), "requestId=abc123")
	require.Contains(t, b.String(), "requestId=abc123")
}

func TestMultiHandlerWithGroupAppliesToEveryChild(t *testing.T) {
	var a, b bytes.Buffer
	m := NewMultiHandler(
		slog.NewTextHandler(&a, &slog.HandlerOptions{Level: slog.LevelDebug}),
		slog.NewTextHandler(&b, &slog.HandlerOptions{Level: slog.LevelDebug}),
	)

	logger := slog.New(m).WithGroup("upload").With("file", "k3s")
	logger.Info("hello")

	for _, out := range []string{a.String(), b.String()} {
		require.Contains(t, out, "upload.file=k3s")
	}
}
