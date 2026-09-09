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

	"github.com/colonel-byte/cargoship/src/api"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/stretchr/testify/require"
)

// newArchValidator builds a ValidateHosts for a package carrying the given architectures. Only the
// distro build metadata is needed: validateHostArch reads nothing else off the manager.
func newArchValidator(t *testing.T, carried api.Arches) *ValidateHosts {
	t.Helper()

	p := &ValidateHosts{}
	p.SetManager(&Manager{
		Distro: &distro.ZarfDistro{
			Build: distro.ZarfDistroBuildData{Architectures: carried},
		},
	})

	return p
}

// newDetectedHost builds a host whose architecture has already been detected. ZarfHost.Arch returns
// the cached value without touching a configurer, which is what the gather-facts phase leaves
// behind by the time validation runs.
func newDetectedHost(t *testing.T, detected string) *cluster.ZarfHost {
	t.Helper()

	h := &cluster.ZarfHost{}
	h.Metadata.Arch = detected

	return h
}

func TestValidateHostArchAcceptsACarriedArch(t *testing.T) {
	for _, tt := range []struct {
		name     string
		carried  api.Arches
		detected string
	}{
		{name: "single architecture package", carried: api.Arches{api.ArchAMD64}, detected: "amd64"},
		{name: "multi architecture package", carried: api.Arches{api.ArchAMD64, api.ArchARM64}, detected: "arm64"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := newArchValidator(t, tt.carried)
			require.NoError(t, p.validateHostArch(context.Background(), newDetectedHost(t, tt.detected)))
		})
	}
}

func TestValidateHostArchRejectsAnArchThePackageDoesNotCarry(t *testing.T) {
	p := newArchValidator(t, api.Arches{api.ArchAMD64, api.ArchRISCV})

	err := p.validateHostArch(context.Background(), newDetectedHost(t, "arm64"))

	require.ErrorContains(t, err, "host architecture arm64 is not carried by this package")
	// The message has to name what the package does carry, so the operator can tell whether the
	// wrong package or the wrong host is the mistake.
	require.ErrorContains(t, err, "amd64, riscv64")
}

// TestValidateHostArchRejectsAnUnsupportedArch covers a host cargoship cannot target at all, such
// as the arm that Linux.Arch reports for a 32-bit machine. This fails on the host rather than on
// the package, so the message must not claim the package is at fault.
func TestValidateHostArchRejectsAnUnsupportedArch(t *testing.T) {
	p := newArchValidator(t, api.Arches{api.ArchAMD64})

	err := p.validateHostArch(context.Background(), newDetectedHost(t, "arm"))

	require.ErrorIs(t, err, api.ErrUnknownArch)
	require.ErrorContains(t, err, `"arm"`)
}

// TestValidateHostArchSkipsAPackageWithoutBuildArches keeps a package assembled before build
// architectures were recorded appliable, rather than failing every host on missing metadata.
func TestValidateHostArchSkipsAPackageWithoutBuildArches(t *testing.T) {
	p := newArchValidator(t, nil)

	require.NoError(t, p.validateHostArch(context.Background(), newDetectedHost(t, "arm64")))
}

// TestValidateHostArchFallsBackToTheScalarArchitecture covers a package built before the
// Architectures list existed, which records only the scalar field. Build.Arches bridges the two, so
// such a package still validates rather than being skipped as metadata-less.
func TestValidateHostArchFallsBackToTheScalarArchitecture(t *testing.T) {
	p := &ValidateHosts{}
	p.SetManager(&Manager{
		Distro: &distro.ZarfDistro{
			Build: distro.ZarfDistroBuildData{Architecture: api.ArchAMD64},
		},
	})

	require.NoError(t, p.validateHostArch(context.Background(), newDetectedHost(t, "amd64")))
	require.Error(t, p.validateHostArch(context.Background(), newDetectedHost(t, "arm64")))
}

// TestValidateHostArchSkipsAManagerWithoutAPackage covers reset, which builds its manager with no
// Distro at all (see cmd/install_reset.go). Resetting a host is not something to gate on the
// architecture of a package that was never loaded, and reading Build off a nil package would panic.
func TestValidateHostArchSkipsAManagerWithoutAPackage(t *testing.T) {
	p := &ValidateHosts{}
	p.SetManager(&Manager{})

	require.NoError(t, p.validateHostArch(context.Background(), newDetectedHost(t, "arm64")))
}
