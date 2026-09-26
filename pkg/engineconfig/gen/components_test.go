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

package gen

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/colonel-byte/cargoship/pkg/engineconfig/extract"
)

func TestGenerateComponents(t *testing.T) {
	src, err := GenerateComponents(ComponentsOptions{
		PackageName: "v1_37",
		Distro:      "rke2",
		Version:     "v1_37",
		Components: extract.Components{
			Disable: []string{"rke2-coredns", "rke2-ingress-nginx"},
			CNI:     []string{"canal", "cilium"},
			Ingress: []string{"traefik"},
		},
	})
	require.NoError(t, err)

	out := string(src)
	require.Contains(t, out, "package v1_37")
	require.Contains(t, out, "var Addons = []string{")
	require.Contains(t, out, `"rke2-ingress-nginx",`)
	require.Contains(t, out, "var CNIs = []string{")
	require.Contains(t, out, `"cilium",`)
	require.Contains(t, out, "var IngressControllers = []string{")
	require.Contains(t, out, `"traefik",`)
}

// A distro that declares no CNI or ingress selection (k3s) still has to emit the vars, or the
// generated registry that references them stops compiling.
func TestGenerateComponentsEmptyListsStillDeclared(t *testing.T) {
	src, err := GenerateComponents(ComponentsOptions{
		PackageName: "v1_37",
		Distro:      "k3s",
		Version:     "v1_37",
		Components:  extract.Components{Disable: []string{"coredns"}},
	})
	require.NoError(t, err)

	out := string(src)
	require.Contains(t, out, "var Addons = []string{")
	require.Contains(t, out, "var CNIs []string")
	require.Contains(t, out, "var IngressControllers []string")
}

func TestGenerateComponentsNothingExtracted(t *testing.T) {
	src, err := GenerateComponents(ComponentsOptions{
		PackageName: "v1_31",
		Distro:      "k3s",
		Version:     "v1_31",
	})
	require.NoError(t, err)

	out := string(src)
	require.Contains(t, out, "var Addons []string")
	require.Contains(t, out, "var CNIs []string")
	require.Contains(t, out, "var IngressControllers []string")
}
