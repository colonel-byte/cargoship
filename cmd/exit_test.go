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

package cmd

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/colonel-byte/cargoship/pkg/phase"
	pkgschema "github.com/colonel-byte/cargoship/pkg/schema"
	"github.com/stretchr/testify/require"
)

// TestExitCode pins the contract. These numbers are a promise to wrapper scripts, so a change here
// should be a deliberate edit to the table rather than a surprise from a refactor.
func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "no error",
			err:  nil,
			want: ExitOK,
		},
		{
			name: "unclassified failure",
			err:  errors.New("something went wrong"),
			want: ExitFailure,
		},
		{
			name: "usage error",
			err:  fmt.Errorf("%w: unknown flag --nope", ErrUsage),
			want: ExitUsage,
		},
		{
			name: "invalid input file",
			err:  fmt.Errorf("%w: bad key", ErrInvalidInput),
			want: ExitInvalidInput,
		},
		{
			name: "schema validation failure is invalid input",
			err:  &pkgschema.ValidationError{},
			want: ExitInvalidInput,
		},
		{
			name: "several schema failures are invalid input",
			err: &multiValidationError{
				files:    2,
				failures: []error{&pkgschema.ValidationError{}},
			},
			want: ExitInvalidInput,
		},
		{
			name: "confirmation gate",
			err:  fmt.Errorf("%w: pass --confirm", ErrConfirmRequired),
			want: ExitConfirmRequired,
		},
		{
			name: "host could not be connected to",
			err:  fmt.Errorf("failed on 1 hosts: %w", phase.ErrConnect),
			want: ExitHostUnreachable,
		},
		{
			name: "interrupted",
			err:  fmt.Errorf("phase stopped: %w", context.Canceled),
			want: ExitInterrupted,
		},
		{
			name: "a wrapped usage error is still a usage error",
			err:  fmt.Errorf("outer: %w", fmt.Errorf("%w: inner", ErrUsage)),
			want: ExitUsage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ExitCode(tt.err))
		})
	}
}

// TestExitCodesAreDistinct guards against two classifications collapsing onto one number, which
// would silently make a wrapper script unable to tell them apart.
func TestExitCodesAreDistinct(t *testing.T) {
	codes := []int{
		ExitOK,
		ExitFailure,
		ExitUsage,
		ExitInvalidInput,
		ExitConfirmRequired,
		ExitHostUnreachable,
		ExitInterrupted,
	}

	seen := make(map[int]bool, len(codes))
	for _, c := range codes {
		require.False(t, seen[c], "exit code %d is used twice", c)
		seen[c] = true
	}
}

// TestRequireConfirmIsClassified ties the gate to the code, so that rewording the message cannot
// quietly drop the classification.
func TestRequireConfirmIsClassified(t *testing.T) {
	err := requireConfirm(false, false)
	require.Error(t, err)
	require.Equal(t, ExitConfirmRequired, ExitCode(err))

	require.NoError(t, requireConfirm(true, false))
	require.NoError(t, requireConfirm(false, true))
}
