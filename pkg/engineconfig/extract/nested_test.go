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

const nestedFixtureSrc = `package v1beta4

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
)

type ClusterConfiguration struct {
	metav1.TypeMeta ` + "`json:\",inline\"`" + `

	Networking Networking ` + "`json:\"networking,omitempty\"`" + `
	APIServer  APIServer  ` + "`json:\"apiServer,omitempty\"`" + `

	ImageRepository string ` + "`json:\"imageRepository,omitempty\"`" + `

	FeatureGates map[string]bool ` + "`json:\"featureGates,omitempty\"`" + `

	Timeouts *Timeouts ` + "`json:\"timeouts,omitempty\"`" + `

	internal string
}

type Networking struct {
	ServiceSubnet string ` + "`json:\"serviceSubnet,omitempty\"`" + `
	PodSubnet     string ` + "`json:\"podSubnet,omitempty\"`" + `
}

type ControlPlaneComponent struct {
	ExtraArgs []Arg ` + "`json:\"extraArgs,omitempty\"`" + `
}

type APIServer struct {
	ControlPlaneComponent ` + "`json:\",inline\"`" + `

	CertSANs []string ` + "`json:\"certSANs,omitempty\"`" + `
}

type Arg struct {
	Name  string ` + "`json:\"name\"`" + `
	Value string ` + "`json:\"value\"`" + `
}

type Timeouts struct {
	ControlPlaneComponentHealthCheck *metav1.Duration ` + "`json:\"controlPlaneComponentHealthCheck,omitempty\"`" + `
}

type NodeRegistrationOptions struct {
	Taints []corev1.Taint ` + "`json:\"taints\"`" + `
}
`

func TestExtractNestedKeysScalarAndBranchFields(t *testing.T) {
	f := mustParse(t, "types.go", nestedFixtureSrc)

	node, err := ExtractNestedKeys(f, "ClusterConfiguration")
	require.NoError(t, err)

	require.Contains(t, node.Children, "imageRepository")
	require.Nil(t, node.Children["imageRepository"].Children, "scalar field should be a leaf")

	require.Contains(t, node.Children, "networking")
	networking := node.Children["networking"]
	require.NotNil(t, networking.Children, "struct-typed field should be a branch")
	require.Contains(t, networking.Children, "podSubnet")
	require.Contains(t, networking.Children, "serviceSubnet")
}

func TestExtractNestedKeysMapFieldIsLeaf(t *testing.T) {
	f := mustParse(t, "types.go", nestedFixtureSrc)

	node, err := ExtractNestedKeys(f, "ClusterConfiguration")
	require.NoError(t, err)

	require.Contains(t, node.Children, "featureGates")
	require.Nil(t, node.Children["featureGates"].Children, "map field should be a leaf")
}

func TestExtractNestedKeysSliceOfStructFieldIsLeaf(t *testing.T) {
	f := mustParse(t, "types.go", nestedFixtureSrc)

	node, err := ExtractNestedKeys(f, "APIServer")
	require.NoError(t, err)

	require.Contains(t, node.Children, "extraArgs", "extraArgs is merged in from the inlined ControlPlaneComponent")
	require.Nil(t, node.Children["extraArgs"].Children, "a []Arg field should not recurse into Arg")
}

func TestExtractNestedKeysPointerToStructIsBranch(t *testing.T) {
	f := mustParse(t, "types.go", nestedFixtureSrc)

	node, err := ExtractNestedKeys(f, "ClusterConfiguration")
	require.NoError(t, err)

	require.Contains(t, node.Children, "timeouts")
	timeouts := node.Children["timeouts"]
	require.NotNil(t, timeouts.Children, "*Timeouts should be a branch")
	require.Contains(t, timeouts.Children, "controlPlaneComponentHealthCheck")
	require.Nil(t, timeouts.Children["controlPlaneComponentHealthCheck"].Children, "*metav1.Duration should be a leaf")
}

func TestExtractNestedKeysInlineEmbeddedLocalStructMerges(t *testing.T) {
	f := mustParse(t, "types.go", nestedFixtureSrc)

	node, err := ExtractNestedKeys(f, "APIServer")
	require.NoError(t, err)

	require.Contains(t, node.Children, "certSANs")
	require.Contains(t, node.Children, "extraArgs", "inlined ControlPlaneComponent's fields should be merged into APIServer")
}

func TestExtractNestedKeysInlineEmbeddedForeignTypeSkipped(t *testing.T) {
	f := mustParse(t, "types.go", nestedFixtureSrc)

	node, err := ExtractNestedKeys(f, "ClusterConfiguration")
	require.NoError(t, err)

	require.NotContains(t, node.Children, "apiVersion", "metav1.TypeMeta is a foreign type and carries no fields this walker can see")
	require.NotContains(t, node.Children, "kind")
}

func TestExtractNestedKeysUnnamedFieldSkipped(t *testing.T) {
	f := mustParse(t, "types.go", nestedFixtureSrc)

	node, err := ExtractNestedKeys(f, "ClusterConfiguration")
	require.NoError(t, err)

	require.NotContains(t, node.Children, "internal", "a field with no json tag has no name to extract")
}

func TestExtractNestedKeysSliceOfForeignStructIsLeaf(t *testing.T) {
	f := mustParse(t, "types.go", nestedFixtureSrc)

	node, err := ExtractNestedKeys(f, "NodeRegistrationOptions")
	require.NoError(t, err)

	require.Contains(t, node.Children, "taints")
	require.Nil(t, node.Children["taints"].Children)
}

func TestExtractNestedKeysUnknownRootType(t *testing.T) {
	f := mustParse(t, "types.go", nestedFixtureSrc)

	_, err := ExtractNestedKeys(f, "DoesNotExist")
	require.Error(t, err)
}
