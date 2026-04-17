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

// Package retry provides simple retry wrappers for functions that return an error
package retry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zarf-dev/zarf/src/pkg/logger"
)

var (
	// DefaultTimeout is a default timeout for retry operations
	DefaultTimeout = 10 * time.Minute
	// Interval is the time to wait between retry attempts
	Interval = 5 * time.Second
	// ErrAbort should be returned when an error occurs on which retrying should be aborted
	ErrAbort = errors.New("retrying aborted")
)

// Context is a retry wrapper that will retry the given function until it succeeds or the context is cancelled
func Context(ctx context.Context, f func(ctx context.Context) error) error {
	var lastErr error

	if ctx.Err() != nil {
		return ctx.Err()
	}

	l := logger.From(ctx)

	// Execute the function immediately for the first try
	lastErr = f(ctx)
	if lastErr == nil || errors.Is(lastErr, ErrAbort) {
		return lastErr
	}

	ticker := time.NewTicker(Interval)
	defer ticker.Stop()

	attempt := 0

	for {
		select {
		case <-ctx.Done():
			l.Info("retry.Context: context cancelled", "attempts", attempt)
			return errors.Join(ctx.Err(), lastErr)
		case <-ticker.C:
			attempt++
			if lastErr != nil {
				l.Debug("retrying", "attempts", attempt, "error", lastErr)
			}
			lastErr = f(ctx)

			if errors.Is(lastErr, ErrAbort) {
				l.Info("retry.Context: aborted", "attempts", attempt)
				return lastErr
			}

			if lastErr == nil {
				l.Info("retry.Context: succeeded", "attempts", attempt)
				return nil
			} else {
				l.Debug("retry.Context: failed", "attempts", attempt, "error", lastErr)
			}
		}
	}
}

// Timeout is a retry wrapper that retries until f succeeds, the context is canceled,
// or the timeout is reached. If timeout <= 0, no additional deadline is set and a
// cancelable child of ctx is used so callers can disable the timeout entirely.
func Timeout(ctx context.Context, timeout time.Duration, f func(ctx context.Context) error) error {
	var (
		child  context.Context
		cancel context.CancelFunc
	)

	if timeout <= 0 {
		child, cancel = context.WithCancel(ctx)
	} else {
		child, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	return Context(child, f)
}

// WithDefaultTimeout wraps f with Timeout using DefaultTimeout.
func WithDefaultTimeout(ctx context.Context, f func(ctx context.Context) error) error {
	return Timeout(ctx, DefaultTimeout, f)
}

// Times is a retry wrapper that will retry the given function until it succeeds or the given number of
// attempts have been made
func Times(ctx context.Context, times int, f func(context.Context) error) error {
	var lastErr error

	// Execute the function immediately for the first try
	lastErr = f(ctx)
	if lastErr == nil || errors.Is(lastErr, ErrAbort) {
		return lastErr
	}

	l := logger.From(ctx)

	i := 1

	ticker := time.NewTicker(Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			l.Info("retry.Times: context cancelled", "attempts", i)
			return errors.Join(ctx.Err(), lastErr)
		case <-ticker.C:
			if lastErr != nil {
				l.Debug("retrying", "attempts", i+1, "times", times, "error", lastErr)
			}

			lastErr = f(ctx)

			if errors.Is(lastErr, ErrAbort) {
				l.Info("retry.Times: aborted", "attempts", i)
				return lastErr
			}

			if lastErr == nil {
				l.Info("retry.Times: succeeded", "attempts", i)
				return nil
			}

			i++

			if i >= times {
				l.Info("retry.Times: exceeded", "attempts", times)
				return fmt.Errorf("retry limit exceeded after %d attempts: %w", times, lastErr)
			}
		}
	}
}
