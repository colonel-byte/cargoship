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

package platform

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateArch(t *testing.T) {
	t.Run("supported architectures pass", func(t *testing.T) {
		for _, a := range []Arch{ArchAMD64, ArchARM64, ArchRISCV} {
			require.NoError(t, ValidateArch(a))
		}
	})

	t.Run("unknown architecture errors", func(t *testing.T) {
		err := ValidateArch(Arch("mips"))
		require.ErrorIs(t, err, ErrUnknownArch)
	})
}
