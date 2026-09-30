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

	"github.com/colonel-byte/cargoship/pkg/engineconfig/extract"
	"github.com/stretchr/testify/require"
)

func TestGenerateNestedKeysRendersValidGo(t *testing.T) {
	node := extract.FieldNode{Children: map[string]extract.FieldNode{
		"imageRepository": {},
		"networking": {Children: map[string]extract.FieldNode{
			"podSubnet":     {},
			"serviceSubnet": {},
		}},
	}}

	src, err := GenerateNestedKeys(NestedKeysOptions{
		PackageName: "v1_35",
		VarName:     "ClusterConfigurationKeys",
		Distro:      "upstream",
		Version:     "v1.35.8",
		RootType:    "ClusterConfiguration",
		Node:        node,
	})

	require.NoError(t, err)
	got := string(src)
	require.Contains(t, got, "package v1_35")
	require.Contains(t, got, "var ClusterConfigurationKeys = extract.FieldNode{")
	require.Contains(t, got, `"imageRepository": extract.FieldNode{}`)
	require.Contains(t, got, `"networking": extract.FieldNode{Children:`)
	require.Contains(t, got, `"podSubnet":`)
}

func TestGenerateNestedKeysEmptyTree(t *testing.T) {
	src, err := GenerateNestedKeys(NestedKeysOptions{
		PackageName: "v1_35",
		VarName:     "ClusterConfigurationKeys",
		Distro:      "upstream",
		Version:     "v1.35.8",
		RootType:    "ClusterConfiguration",
		Node:        extract.FieldNode{Children: map[string]extract.FieldNode{}},
	})

	require.NoError(t, err)
	require.Contains(t, string(src), "var ClusterConfigurationKeys = extract.FieldNode{Children: map[string]extract.FieldNode{}}")
}
