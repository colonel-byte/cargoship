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

package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArchValidate(t *testing.T) {
	t.Run("supported architectures", func(t *testing.T) {
		for _, a := range []Arch{ArchAMD64, ArchARM64, ArchRISCV} {
			require.NoErrorf(t, a.Validate(), "expected %q to be valid", a)
		}
	})

	t.Run("unsupported architecture", func(t *testing.T) {
		err := Arch("mips").Validate()
		require.Error(t, err)
		require.ErrorIs(t, err, ErrUnknownArch)
		require.ErrorContains(t, err, "mips")
	})

	t.Run("empty architecture", func(t *testing.T) {
		err := Arch("").Validate()
		require.Error(t, err)
		require.ErrorIs(t, err, ErrUnknownArch)
	})
}

func TestParseArch(t *testing.T) {
	t.Run("supported architecture", func(t *testing.T) {
		a, err := ParseArch("arm64")
		require.NoError(t, err)
		require.Equal(t, ArchARM64, a)
	})

	t.Run("unsupported architecture returns error and zero value", func(t *testing.T) {
		a, err := ParseArch("mips")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrUnknownArch)
		require.Equal(t, Arch(""), a)
	})
}

func TestFormatArches(t *testing.T) {
	t.Run("multiple architectures", func(t *testing.T) {
		require.Equal(t, "amd64, arm64", FormatArches(Arches{ArchAMD64, ArchARM64}))
	})

	t.Run("single architecture", func(t *testing.T) {
		require.Equal(t, "amd64", FormatArches(Arches{ArchAMD64}))
	})

	t.Run("empty list", func(t *testing.T) {
		require.Empty(t, FormatArches(Arches{}))
	})
}
