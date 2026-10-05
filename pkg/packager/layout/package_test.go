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

package layout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/signing"
)

func TestDigest(t *testing.T) {
	d := &DistroLayout{digest: "sha256:abc"}
	require.Equal(t, "sha256:abc", d.Digest())
}

func TestIsSigned(t *testing.T) {
	t.Run("nil signed field defaults to false", func(t *testing.T) {
		d := &DistroLayout{}
		require.False(t, d.IsSigned())
	})

	t.Run("true when metadata says signed", func(t *testing.T) {
		signed := true
		d := &DistroLayout{Distro: distro.ZarfDistro{Build: distro.ZarfDistroBuildData{Signed: &signed}}}
		require.True(t, d.IsSigned())
	})

	t.Run("false when metadata says not signed", func(t *testing.T) {
		signed := false
		d := &DistroLayout{Distro: distro.ZarfDistro{Build: distro.ZarfDistroBuildData{Signed: &signed}}}
		require.False(t, d.IsSigned())
	})
}

func TestDistroLayoutFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nested", "b.txt"), []byte("b"), 0o600))

	d := &DistroLayout{dirPath: dir}
	files, err := d.Files()
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, "a.txt", files[filepath.Join(dir, "a.txt")])
	require.Equal(t, "nested/b.txt", files[filepath.Join(dir, "nested", "b.txt")])
}

// writeChecksummedPackage builds a minimal valid, on-disk package directory: a distro.yaml,
// one data file, and a checksums.txt covering it, with AggregateChecksum set to match. It
// returns the directory and the ZarfDistro whose AggregateChecksum matches the checksums file,
// so validateDistroIntegrity succeeds unless the caller mutates the fixture further.
func writeChecksummedPackage(t *testing.T) (string, distro.ZarfDistro) {
	t.Helper()
	dir := t.TempDir()

	dataPath := filepath.Join(dir, "data.txt")
	require.NoError(t, os.WriteFile(dataPath, []byte("hello"), 0o600))
	dataSum, err := helpers.GetSHA256OfFile(dataPath)
	require.NoError(t, err)

	checksumsContent := fmt.Sprintf("%s %s\n", dataSum, "data.txt")
	checksumsPath := filepath.Join(dir, config.Checksums)
	require.NoError(t, os.WriteFile(checksumsPath, []byte(checksumsContent), 0o600))
	aggSum, err := helpers.GetSHA256OfFile(checksumsPath)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, config.DistroYAML), []byte("metadata:\n  name: test-package\n"), 0o600))

	dis := distro.ZarfDistro{
		Metadata: distro.ZarfDistroMetadata{
			Name:              "test-package",
			Version:           "1.0.0",
			AggregateChecksum: aggSum,
		},
	}
	return dir, dis
}

func TestValidateDistroIntegrity(t *testing.T) {
	t.Run("missing distro.yaml", func(t *testing.T) {
		dir, dis := writeChecksummedPackage(t)
		require.NoError(t, os.Remove(filepath.Join(dir, config.DistroYAML)))
		err := validateDistroIntegrity(&DistroLayout{dirPath: dir, Distro: dis})
		require.Error(t, err)
	})

	t.Run("missing checksums.txt", func(t *testing.T) {
		dir, dis := writeChecksummedPackage(t)
		require.NoError(t, os.Remove(filepath.Join(dir, config.Checksums)))
		err := validateDistroIntegrity(&DistroLayout{dirPath: dir, Distro: dis})
		require.Error(t, err)
	})

	t.Run("aggregate checksum mismatch", func(t *testing.T) {
		dir, dis := writeChecksummedPackage(t)
		dis.Metadata.AggregateChecksum = "0000000000000000000000000000000000000000000000000000000000000000"[:64]
		err := validateDistroIntegrity(&DistroLayout{dirPath: dir, Distro: dis})
		require.Error(t, err)
		require.ErrorContains(t, err, "expected sha256")
	})

	t.Run("file content does not match its checksum", func(t *testing.T) {
		dir, dis := writeChecksummedPackage(t)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "data.txt"), []byte("tampered"), 0o600))
		err := validateDistroIntegrity(&DistroLayout{dirPath: dir, Distro: dis})
		require.Error(t, err)
	})

	t.Run("extra file not covered by checksums", func(t *testing.T) {
		dir, dis := writeChecksummedPackage(t)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("surprise"), 0o600))
		err := validateDistroIntegrity(&DistroLayout{dirPath: dir, Distro: dis})
		require.Error(t, err)
		require.ErrorContains(t, err, "additional files not present in the checksum")
	})

	t.Run("malformed checksum line", func(t *testing.T) {
		dir, dis := writeChecksummedPackage(t)
		checksumsPath := filepath.Join(dir, config.Checksums)
		require.NoError(t, os.WriteFile(checksumsPath, []byte("not-a-valid-line\n"), 0o600))
		aggSum, err := helpers.GetSHA256OfFile(checksumsPath)
		require.NoError(t, err)
		dis.Metadata.AggregateChecksum = aggSum

		err = validateDistroIntegrity(&DistroLayout{dirPath: dir, Distro: dis})
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid checksum line")
	})

	t.Run("valid package passes", func(t *testing.T) {
		dir, dis := writeChecksummedPackage(t)
		err := validateDistroIntegrity(&DistroLayout{dirPath: dir, Distro: dis})
		require.NoError(t, err)
	})

	t.Run("unsafe metadata name fails path validation", func(t *testing.T) {
		dir, dis := writeChecksummedPackage(t)
		dis.Metadata.Name = "../escape"
		err := validateDistroIntegrity(&DistroLayout{dirPath: dir, Distro: dis})
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid path")
	})
}

func TestLoadFromDirErrors(t *testing.T) {
	t.Run("missing distro.yaml", func(t *testing.T) {
		dir := t.TempDir()
		_, err := LoadFromDir(context.Background(), dir, DistroLayoutOptions{})
		require.Error(t, err)
	})

	t.Run("invalid distro.yaml", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, config.DistroYAML), []byte("not: [valid"), 0o600))
		_, err := LoadFromDir(context.Background(), dir, DistroLayoutOptions{})
		require.Error(t, err)
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		_, err := LoadFromDir(context.Background(), filepath.Join(t.TempDir(), "missing"), DistroLayoutOptions{})
		require.Error(t, err)
	})
}

func TestLoadFromTarErrors(t *testing.T) {
	_, err := LoadFromTar(context.Background(), filepath.Join(t.TempDir(), "missing.tar.zst"), DistroLayoutOptions{})
	require.Error(t, err)
}

func TestValues(t *testing.T) {
	dir := t.TempDir()
	d := &DistroLayout{dirPath: dir, Distro: distro.ZarfDistro{}}
	values, err := d.Values(context.Background())
	require.NoError(t, err)
	require.Empty(t, values)
}

// A SignPackage call whose options carry no key material is a no-op, so the gate
// deciding that is the difference between an unsigned package and a failed build.
// Zarf owned this check until v0.87.0 removed SignBlobOptions.ShouldSign along
// with its Keyless field; the keyless row is the one that cannot be recovered
// from the cosign options alone, since --keyless is mutually exclusive with
// --signing-key and every other keyless flag defaults to empty.
func TestSignOptionsShouldSign(t *testing.T) {
	for _, tt := range []struct {
		name string
		opts SignOptions
		want bool
	}{
		{
			name: "nothing configured",
			opts: SignOptions{},
			want: false,
		},
		{
			name: "signing key path",
			opts: SignOptions{
				SignBlobOptions: signing.SignBlobOptions{Key: "cosign.key"},
			},
			want: true,
		},
		{
			name: "deprecated key reference",
			opts: SignOptions{
				SignBlobOptions: signing.SignBlobOptions{KeyRef: "cosign.key"}, //nolint:staticcheck // the deprecated field is what this row covers
			},
			want: true,
		},
		{
			name: "fulcio identity token",
			opts: func() SignOptions {
				var o SignOptions
				o.Fulcio.IdentityToken = "a-token"
				return o
			}(),
			want: true,
		},
		{
			name: "hardware security key",
			opts: func() SignOptions {
				var o SignOptions
				o.SecurityKey.Use = true
				return o
			}(),
			want: true,
		},
		{
			name: "keyless with no other material",
			opts: SignOptions{
				Keyless: true,
			},
			want: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.opts.ShouldSign())
		})
	}
}
