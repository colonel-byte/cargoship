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
	"testing"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

// TestInstallPackagesRunsTheInstallOnce holds the property InstallStagedFiles depends on: a host
// whose install has run is indistinguishable from one that was never staged unless running it
// clears it. Without that, the catch-all phase would move an already-promoted binary a second
// time and report the failure of a `mv` whose source no longer exists.
func TestInstallPackagesRunsTheInstallOnce(t *testing.T) {
	p := &GenericPhase{}
	h := &cluster.Host{}

	calls := 0
	h.Metadata.Install = func(_ context.Context, _ *cluster.Host) error {
		calls++
		return nil
	}

	require.NoError(t, p.installPackages(t.Context(), h))
	require.Equal(t, 1, calls, "the install did not run")
	require.Nil(t, h.Metadata.Install, "the install was left on the host, so it can run twice")

	require.NoError(t, p.installPackages(t.Context(), h))
	require.Equal(t, 1, calls, "the install ran again after it had been consumed")
}

// TestInstallPackagesClearsTheInstallEvenWhenItFails keeps a failing install from being retried by
// the catch-all phase in the same run. The upload phases warn and carry on rather than failing an
// apply, so a second attempt would produce a second warning about the same host and nothing else:
// the next run stages again, which is where the retry belongs.
func TestInstallPackagesClearsTheInstallEvenWhenItFails(t *testing.T) {
	p := &GenericPhase{}
	h := &cluster.Host{}

	calls := 0
	h.Metadata.Install = func(_ context.Context, _ *cluster.Host) error {
		calls++
		return context.DeadlineExceeded
	}

	require.Error(t, p.installPackages(t.Context(), h))
	require.Equal(t, 1, calls)
	require.Nil(t, h.Metadata.Install)
}

// TestInstallPackagesOnAHostWithNothingStaged covers the host the package carries no files for --
// a Debian host and a package with no deb, say. It is not an error and nothing should run.
func TestInstallPackagesOnAHostWithNothingStaged(t *testing.T) {
	p := &GenericPhase{}
	h := &cluster.Host{}

	require.NoError(t, p.installPackages(t.Context(), h))
	require.Nil(t, h.Metadata.Install)
}
