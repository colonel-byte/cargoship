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

const k3sOptsSrc = `package cmds

var (
	copyFlag   = &K3SFlagOption{}
	dropFlag   = &K3SFlagOption{Drop: true}
	hideFlag   = &K3SFlagOption{Hide: true}
	ignoreFlag = &K3SFlagOption{Ignore: true}
)
`

func TestParseK3SFlagSetIdentifierAndElidedLiteral(t *testing.T) {
	optsFile := mustParse(t, "k3sopts.go", k3sOptsSrc)
	optIndex := BuildK3SFlagOptionIndex(optsFile)
	require.Equal(t, K3SFlagOption{}, optIndex["copyFlag"])
	require.Equal(t, K3SFlagOption{Drop: true}, optIndex["dropFlag"])
	require.Equal(t, K3SFlagOption{Hide: true}, optIndex["hideFlag"])

	src := `package cmds

var k3sServerBase = mustCmdFromK3S(cmds.NewServerCommand(ServerRun), K3SFlagSet{
	"cluster-cidr": copyFlag,
	"v":            hideFlag,
	"docker":       dropFlag,
	"data-dir": {
		Usage: "(data) Folder to hold state",
	},
})
`
	file := mustParse(t, "server.go", src)
	mapLit := FindK3SFlagSet(file)
	require.NotNil(t, mapLit)

	flagSet, err := ParseK3SFlagSet(optIndex, mapLit)
	require.NoError(t, err)
	require.Equal(t, K3SFlagOption{}, flagSet["cluster-cidr"])
	require.Equal(t, K3SFlagOption{Hide: true}, flagSet["v"])
	require.Equal(t, K3SFlagOption{Drop: true}, flagSet["docker"])
	require.Equal(t, K3SFlagOption{}, flagSet["data-dir"]) // elided literal, no Drop/Hide set
}

func TestParseK3SFlagSetUnresolvedIdentifierErrors(t *testing.T) {
	src := `package cmds

var k3sServerBase = mustCmdFromK3S(cmds.NewServerCommand(ServerRun), K3SFlagSet{
	"cluster-cidr": someUnknownVar,
})
`
	file := mustParse(t, "server.go", src)
	mapLit := FindK3SFlagSet(file)
	require.NotNil(t, mapLit)

	_, err := ParseK3SFlagSet(map[string]K3SFlagOption{}, mapLit)
	require.Error(t, err)
}

func TestApplyK3SFlagSetDropHideCopy(t *testing.T) {
	k3sFlags := []Flag{
		{Name: "cluster-cidr", Type: "string", Slice: true},
		{Name: "docker", Type: "string"},
		{Name: "v", Type: "string", Hidden: false},
		{Name: "no-entry-for-this-one", Type: "string"},
		{Name: "DebugFlag", Unresolved: true},
	}
	flagSet := map[string]K3SFlagOption{
		"cluster-cidr": {},
		"docker":       {Drop: true},
		"v":            {Hide: true},
	}

	flags, unmapped := ApplyK3SFlagSet(k3sFlags, flagSet)

	require.Equal(t, []string{"no-entry-for-this-one"}, unmapped)

	names := make([]string, len(flags))
	for i, f := range flags {
		names[i] = f.Name
	}
	require.Equal(t, []string{"cluster-cidr", "v", "no-entry-for-this-one", "DebugFlag"}, names)

	for _, f := range flags {
		if f.Name == "v" {
			require.True(t, f.Hidden)
		}
	}
}

func TestBuildFlagNonLiteralNameFallsBackToUnresolved(t *testing.T) {
	src := `package cmds

import "github.com/urfave/cli/v2"

var commonFlag = []cli.Flag{
	&cli.StringFlag{
		Name: images.KubeAPIServer,
	},
	&cli.StringFlag{
		Name: podtemplate.KubeAPIServer + "-extra-mount",
	},
}
`
	file := mustParse(t, "root.go", src)
	flags, err := ExtractFlags(BuildVarIndex(file), file)
	require.NoError(t, err)
	require.Len(t, flags, 2)

	require.True(t, flags[0].Unresolved)
	require.Equal(t, "images.KubeAPIServer", flags[0].Name)

	require.True(t, flags[1].Unresolved)
	require.Equal(t, `podtemplate.KubeAPIServer + "-extra-mount"`, flags[1].Name)
}
