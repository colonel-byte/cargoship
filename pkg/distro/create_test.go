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

package distro

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateMissingDistroPath(t *testing.T) {
	ctx := context.Background()
	distroPath := filepath.Join(t.TempDir(), "does-not-exist")

	_, err := Create(ctx, distroPath, t.TempDir(), CreateOptions{})
	require.Error(t, err)
}

func TestCreateInvalidManifest(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "distro.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte("not: [valid"), 0o600))

	_, err := Create(ctx, dir, t.TempDir(), CreateOptions{})
	require.Error(t, err)
}
