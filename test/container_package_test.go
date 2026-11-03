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

package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExampleDirDefaultsToTheShippedDirectory(t *testing.T) {
	t.Setenv(exampleRootEnvVar, "")
	require.Equal(t, defaultExampleDir, ExampleDir())
}

func TestExampleDirHonoursTheOverride(t *testing.T) {
	t.Setenv(exampleRootEnvVar, "/srv/cargoship/example")
	require.Equal(t, "/srv/cargoship/example", ExampleDir())
}

// TestContainerSafeDefinitionUnderAnOverriddenRoot is the case the override exists for: a suite in
// another module, whose own repository has no example/ directory, pointing at the example
// directory of a cargoship checkout by absolute path.
//
// The definition's path below the root is what has to survive, because that is what keeps a
// source reaching outside its own directory resolvable in the copy -- see ContainerSafeDefinition.
func TestContainerSafeDefinitionUnderAnOverriddenRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(exampleRootEnvVar, root)

	// Two levels deep, and reaching one level up for a shared file, which is the shape the k3s
	// definitions have.
	definition := filepath.Join(root, "k3s-flannel", "v1_36")
	require.NoError(t, os.MkdirAll(filepath.Join(definition, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(definition, "distro.yaml"),
		[]byte("files:\n  - source: ../shared/killall.sh\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "k3s-flannel", "shared"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "k3s-flannel", "shared", "killall.sh"),
		[]byte("#!/bin/sh\n"), 0o700))

	dst := t.TempDir()
	got, err := ContainerSafeDefinition(definition, dst)
	require.NoError(t, err)

	require.Equal(t, filepath.Join(dst, "k3s-flannel", "v1_36"), got,
		"the copy did not keep the definition's path below the example root")
	require.FileExists(t, filepath.Join(got, "distro.yaml"))
	require.FileExists(t, filepath.Join(dst, "k3s-flannel", "shared", "killall.sh"),
		"the source the definition reaches outside itself for was not copied beside it")
}

// TestContainerSafeDefinitionRefusesADefinitionOutsideTheRoot keeps the error that explains the
// mistake rather than a copy whose relative sources resolve nowhere.
func TestContainerSafeDefinitionRefusesADefinitionOutsideTheRoot(t *testing.T) {
	t.Setenv(exampleRootEnvVar, t.TempDir())

	_, err := ContainerSafeDefinition(filepath.Join(t.TempDir(), "elsewhere"), t.TempDir())
	require.ErrorContains(t, err, "so its relative sources cannot be preserved")
}
