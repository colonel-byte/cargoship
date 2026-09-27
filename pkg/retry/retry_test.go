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

package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// withShortInterval temporarily shrinks the package-level retry Interval so tests that
// exercise the retry loop don't wait on the real 15s cadence. It is not safe for
// parallel tests since Interval is a shared package variable.
func withShortInterval(t *testing.T) {
	t.Helper()
	original := Interval
	Interval = time.Millisecond
	t.Cleanup(func() { Interval = original })
}

func TestContext(t *testing.T) {
	t.Run("succeeds on first try without retrying", func(t *testing.T) {
		calls := 0
		err := Context(context.Background(), func(_ context.Context) error {
			calls++
			return nil
		})
		require.NoError(t, err)
		require.Equal(t, 1, calls)
	})

	t.Run("context already canceled returns immediately without calling f", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		called := false
		err := Context(ctx, func(_ context.Context) error {
			called = true
			return nil
		})
		require.Error(t, err)
		require.False(t, called)
	})

	t.Run("ErrAbort on first try returns immediately without retrying", func(t *testing.T) {
		withShortInterval(t)
		calls := 0
		err := Context(context.Background(), func(_ context.Context) error {
			calls++
			return ErrAbort
		})
		require.ErrorIs(t, err, ErrAbort)
		require.Equal(t, 1, calls)
	})

	t.Run("retries until success", func(t *testing.T) {
		withShortInterval(t)
		calls := 0
		err := Context(context.Background(), func(_ context.Context) error {
			calls++
			if calls < 3 {
				return errors.New("not yet")
			}
			return nil
		})
		require.NoError(t, err)
		require.Equal(t, 3, calls)
	})

	t.Run("ErrAbort during a retry stops immediately", func(t *testing.T) {
		withShortInterval(t)
		calls := 0
		err := Context(context.Background(), func(_ context.Context) error {
			calls++
			if calls < 2 {
				return errors.New("transient")
			}
			return ErrAbort
		})
		require.ErrorIs(t, err, ErrAbort)
		require.Equal(t, 2, calls)
	})

	t.Run("context canceled mid-retry returns joined error", func(t *testing.T) {
		withShortInterval(t)
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		err := Context(ctx, func(_ context.Context) error {
			calls++
			if calls == 2 {
				cancel()
			}
			return errors.New("still failing")
		})
		require.Error(t, err)
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorContains(t, err, "still failing")
	})
}

func TestTimeout(t *testing.T) {
	t.Run("succeeds within timeout", func(t *testing.T) {
		err := Timeout(context.Background(), time.Second, func(_ context.Context) error {
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("zero timeout does not set a deadline", func(t *testing.T) {
		var sawDeadline bool
		err := Timeout(context.Background(), 0, func(ctx context.Context) error {
			_, sawDeadline = ctx.Deadline()
			return nil
		})
		require.NoError(t, err)
		require.False(t, sawDeadline)
	})

	t.Run("times out while f keeps failing", func(t *testing.T) {
		withShortInterval(t)
		err := Timeout(context.Background(), 5*time.Millisecond, func(_ context.Context) error {
			return errors.New("keeps failing")
		})
		require.Error(t, err)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

func TestWithDefaultTimeout(t *testing.T) {
	t.Run("succeeds without needing DefaultTimeout to elapse", func(t *testing.T) {
		err := WithDefaultTimeout(context.Background(), func(_ context.Context) error {
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("uses DefaultTimeout as the deadline budget", func(t *testing.T) {
		original := DefaultTimeout
		DefaultTimeout = 5 * time.Millisecond
		t.Cleanup(func() { DefaultTimeout = original })
		withShortInterval(t)

		err := WithDefaultTimeout(context.Background(), func(_ context.Context) error {
			return errors.New("keeps failing")
		})
		require.Error(t, err)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

func TestTimes(t *testing.T) {
	t.Run("succeeds on first try without retrying", func(t *testing.T) {
		calls := 0
		err := Times(context.Background(), 5, func(_ context.Context) error {
			calls++
			return nil
		})
		require.NoError(t, err)
		require.Equal(t, 1, calls)
	})

	t.Run("ErrAbort on first try returns immediately", func(t *testing.T) {
		calls := 0
		err := Times(context.Background(), 5, func(_ context.Context) error {
			calls++
			return ErrAbort
		})
		require.ErrorIs(t, err, ErrAbort)
		require.Equal(t, 1, calls)
	})

	t.Run("retries until success within the attempt budget", func(t *testing.T) {
		withShortInterval(t)
		calls := 0
		err := Times(context.Background(), 5, func(_ context.Context) error {
			calls++
			if calls < 3 {
				return errors.New("not yet")
			}
			return nil
		})
		require.NoError(t, err)
		require.Equal(t, 3, calls)
	})

	t.Run("exceeds attempt limit and returns an error", func(t *testing.T) {
		withShortInterval(t)
		calls := 0
		err := Times(context.Background(), 3, func(_ context.Context) error {
			calls++
			return errors.New("always fails")
		})
		require.Error(t, err)
		require.ErrorContains(t, err, "retry limit exceeded")
		require.ErrorContains(t, err, "always fails")
	})

	t.Run("ErrAbort during a retry stops immediately", func(t *testing.T) {
		withShortInterval(t)
		calls := 0
		err := Times(context.Background(), 10, func(_ context.Context) error {
			calls++
			if calls < 2 {
				return errors.New("transient")
			}
			return ErrAbort
		})
		require.ErrorIs(t, err, ErrAbort)
		require.Equal(t, 2, calls)
	})

	t.Run("context canceled mid-retry returns joined error", func(t *testing.T) {
		withShortInterval(t)
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		err := Times(ctx, 10, func(_ context.Context) error {
			calls++
			if calls == 2 {
				cancel()
			}
			return errors.New("still failing")
		})
		require.Error(t, err)
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorContains(t, err, "still failing")
	})
}
