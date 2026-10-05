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

package extract

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStringListDeclCommaSeparatedConst(t *testing.T) {
	// k3s's shape: pkg/cli/cmds/stage.go.
	src := `package cmds

const (
	DisableItems = "coredns, servicelb, traefik, local-storage, metrics-server, runtimes"
)
`
	items, ok := StringListDecl("DisableItems", mustParse(t, "stage.go", src))
	require.True(t, ok)
	require.Equal(t, []string{"coredns", "servicelb", "traefik", "local-storage", "metrics-server", "runtimes"}, items)
}

func TestStringListDeclStringSliceVar(t *testing.T) {
	// rke2's shape: pkg/cli/types.go.
	src := `package cli

var (
	DisableItems = []string{"rke2-coredns", "rke2-metrics-server"}
	CNIItems     = []string{"calico", "canal", "cilium", "flannel"}
)
`
	file := mustParse(t, "types.go", src)

	disable, ok := StringListDecl("DisableItems", file)
	require.True(t, ok)
	require.Equal(t, []string{"rke2-coredns", "rke2-metrics-server"}, disable)

	cni, ok := StringListDecl("CNIItems", file)
	require.True(t, ok)
	require.Equal(t, []string{"calico", "canal", "cilium", "flannel"}, cni)
}

func TestStringListDeclSkipsNonLiteralElements(t *testing.T) {
	src := `package cli

var DisableItems = []string{"rke2-coredns", someConst, "rke2-metrics-server"}
`
	items, ok := StringListDecl("DisableItems", mustParse(t, "types.go", src))
	require.True(t, ok)
	require.Equal(t, []string{"rke2-coredns", "rke2-metrics-server"}, items)
}

func TestStringListDeclMissing(t *testing.T) {
	src := `package cli

var CNIItems = []string{"canal"}
`
	items, ok := StringListDecl("DisableItems", mustParse(t, "types.go", src))
	require.False(t, ok)
	require.Nil(t, items)
}

func TestStringListDeclWrongShape(t *testing.T) {
	src := `package cli

var DisableItems = []int{1, 2}
`
	items, ok := StringListDecl("DisableItems", mustParse(t, "types.go", src))
	require.False(t, ok)
	require.Nil(t, items)
}

func TestStringListDeclAcrossFiles(t *testing.T) {
	a := mustParse(t, "server.go", "package cmds\n\nvar CNIItems = []string{\"canal\"}\n")
	b := mustParse(t, "types.go", "package cmds\n\nconst DisableItems = \"coredns, servicelb\"\n")

	items, ok := StringListDecl("DisableItems", a, b)
	require.True(t, ok)
	require.Equal(t, []string{"coredns", "servicelb"}, items)
}

func TestDisableSetCalls(t *testing.T) {
	src := `package cmds

func validateCNI(clx *cli.Context) {
	clx.Set("disable", "rke2-multus")
	clx.Set("disable", "rke2-multus-crd")
	clx.Set("disable", someVar)
	clx.Set("disable-cloud-controller", "true")
	clx.Set("cni")
}
`
	values := DisableSetCalls(mustParse(t, "server.go", src))
	require.Equal(t, []string{"rke2-multus", "rke2-multus-crd"}, values)
}

func TestDisableSetCallsNone(t *testing.T) {
	src := `package cmds

func noop() {}
`
	require.Empty(t, DisableSetCalls(mustParse(t, "agent.go", src)))
}
