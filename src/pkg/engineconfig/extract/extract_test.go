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
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustParse(t *testing.T, name, src string) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	require.NoError(t, err)
	return f
}

func TestExtractFlagsInlineLiteral(t *testing.T) {
	src := `package cmds

import "github.com/urfave/cli/v2"

var ServerFlags = []cli.Flag{
	&cli.StringFlag{
		Name:  "cluster-domain",
		Value: "cluster.local",
	},
}
`
	file := mustParse(t, "server.go", src)
	flags, err := ExtractFlags(BuildVarIndex(file), file)
	require.NoError(t, err)
	require.Len(t, flags, 1)
	require.Equal(t, Flag{Name: "cluster-domain", Type: "string", Value: "cluster.local"}, flags[0])
}

func TestExtractFlagsIndirectVarReference(t *testing.T) {
	src := `package cmds

import "github.com/urfave/cli/v2"

var (
	ServerConfig Server
	ClusterCIDR  = &cli.StringSliceFlag{
		Name:        "cluster-cidr",
		Destination: &ServerConfig.ClusterCIDR,
	}
)

var ServerFlags = []cli.Flag{
	ClusterCIDR,
}
`
	file := mustParse(t, "server.go", src)
	flags, err := ExtractFlags(BuildVarIndex(file), file)
	require.NoError(t, err)
	require.Len(t, flags, 1)
	require.Equal(t, "cluster-cidr", flags[0].Name)
	require.Equal(t, "string", flags[0].Type)
	require.True(t, flags[0].Slice)
	require.Equal(t, "&ServerConfig.ClusterCIDR", flags[0].Destination)
	require.False(t, flags[0].Unresolved)
}

func TestExtractFlagsUnresolvedIdentifier(t *testing.T) {
	src := `package cmds

import "github.com/urfave/cli/v2"

var ServerFlags = []cli.Flag{
	DebugFlag,
}
`
	file := mustParse(t, "server.go", src)
	flags, err := ExtractFlags(BuildVarIndex(file), file)
	require.NoError(t, err)
	require.Len(t, flags, 1)
	require.Equal(t, Flag{Name: "DebugFlag", Unresolved: true}, flags[0])
}

func TestExtractFlagsSliceType(t *testing.T) {
	src := `package cmds

import "github.com/urfave/cli/v2"

var ServerFlags = []cli.Flag{
	&cli.StringSliceFlag{
		Name: "tls-san",
	},
}
`
	file := mustParse(t, "server.go", src)
	flags, err := ExtractFlags(BuildVarIndex(file), file)
	require.NoError(t, err)
	require.Len(t, flags, 1)
	require.Equal(t, "string", flags[0].Type)
	require.True(t, flags[0].Slice)
}

func TestExtractFlagsHiddenFlag(t *testing.T) {
	src := `package cmds

import "github.com/urfave/cli/v2"

var ServerFlags = []cli.Flag{
	&cli.BoolFlag{
		Name:   "disable-agent",
		Hidden: true,
	},
}
`
	file := mustParse(t, "server.go", src)
	flags, err := ExtractFlags(BuildVarIndex(file), file)
	require.NoError(t, err)
	require.Len(t, flags, 1)
	require.True(t, flags[0].Hidden)
}

func TestExtractFlagsAliasViaSharedDestination(t *testing.T) {
	src := `package cmds

import "github.com/urfave/cli/v2"

var (
	ServerConfig        Server
	ExtraControllerArgs = &cli.StringSliceFlag{
		Name:        "kube-controller-manager-arg",
		Destination: &ServerConfig.ExtraControllerArgs,
	}
)

var ServerFlags = []cli.Flag{
	ExtraControllerArgs,
	&cli.StringSliceFlag{
		Hidden:      true,
		Name:        "kube-controller-arg",
		Destination: &ServerConfig.ExtraControllerArgs,
	},
}
`
	file := mustParse(t, "server.go", src)
	flags, err := ExtractFlags(BuildVarIndex(file), file)
	require.NoError(t, err)
	require.Len(t, flags, 2)

	require.Equal(t, "kube-controller-manager-arg", flags[0].Name)
	require.Empty(t, flags[0].AliasOf)

	require.Equal(t, "kube-controller-arg", flags[1].Name)
	require.True(t, flags[1].Hidden)
	require.Equal(t, "kube-controller-manager-arg", flags[1].AliasOf)
}

func TestExtractFlagsNonLiteralValueKeptAsSourceText(t *testing.T) {
	src := `package cmds

import (
	"time"

	"github.com/urfave/cli/v2"
)

var ServerFlags = []cli.Flag{
	&cli.DurationFlag{
		Name:  "etcd-s3-timeout",
		Value: 5 * time.Minute,
	},
}
`
	file := mustParse(t, "server.go", src)
	flags, err := ExtractFlags(BuildVarIndex(file), file)
	require.NoError(t, err)
	require.Len(t, flags, 1)
	require.Equal(t, "5 * time.Minute", flags[0].Value)
}

func TestExtractFlagsNoFlagsListReturnsError(t *testing.T) {
	src := `package cmds

var NotAFlagList = []string{"a", "b"}
`
	file := mustParse(t, "server.go", src)
	_, err := ExtractFlags(BuildVarIndex(file), file)
	require.Error(t, err)
}

func TestBuildVarIndexAcrossFiles(t *testing.T) {
	serverSrc := `package cmds

import "github.com/urfave/cli/v2"

var ServerFlags = []cli.Flag{
	SELinuxFlag,
}
`
	agentSrc := `package cmds

import "github.com/urfave/cli/v2"

var SELinuxFlag = &cli.BoolFlag{
	Name: "selinux",
}
`
	serverFile := mustParse(t, "server.go", serverSrc)
	agentFile := mustParse(t, "agent.go", agentSrc)

	idx := BuildVarIndex(serverFile, agentFile)
	flags, err := ExtractFlags(idx, serverFile)
	require.NoError(t, err)
	require.Len(t, flags, 1)
	require.Equal(t, "selinux", flags[0].Name)
	require.False(t, flags[0].Unresolved)
}
