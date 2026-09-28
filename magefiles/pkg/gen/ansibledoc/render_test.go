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

package ansibledoc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderProse(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"code":                   {"Pass C(--dry-run) here.", "Pass `--dry-run` here."},
		"module":                 {"See M(colonel_byte.cargoship.apply).", "See `colonel_byte.cargoship.apply`."},
		"bold":                   {"This is B(required).", "This is **required**."},
		"italic":                 {"This is I(optional).", "This is *optional*."},
		"several in one line":    {"B(No) C(--force).", "**No** `--force`."},
		"plain prose is left be": {"Nothing to rewrite.", "Nothing to rewrite."},
		// An unhandled form comes through verbatim rather than vanishing, which is what makes a
		// new markup form show up as a visible bug on the page.
		"an unhandled form": {"See L(the docs, https://x).", "See L(the docs, https://x)."},
		// The word boundary is what keeps a capital inside a longer word from being treated as
		// markup.
		"a letter mid-word": {"ABC(x)", "ABC(x)"},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, renderProse(tc.in))
		})
	}
}

func TestRenderDescription(t *testing.T) {
	t.Run("joins the lines into one cell", func(t *testing.T) {
		got := renderDescription(namedOption{docOption: docOption{
			Description: []string{"First sentence.", "Second with C(markup)."},
		}})
		require.Equal(t, "First sentence. Second with `markup`.", got)
	})

	t.Run("appends the choices", func(t *testing.T) {
		got := renderDescription(namedOption{docOption: docOption{
			Description: []string{"Pick one."},
			Choices:     []string{"a", "b"},
		}})
		require.Equal(t, "Pick one. One of `a`, `b`.", got)
	})
}

func TestRenderType(t *testing.T) {
	require.Equal(t, "`str`", renderType(namedOption{docOption: docOption{Type: "str"}}))
	require.Equal(t, "`list` of `str`",
		renderType(namedOption{docOption: docOption{Type: "list", Elements: "str"}}))
}

// TestRenderCLIFlag covers the two spellings of "this option renders no flag": the key being
// absent and the documented literal.
func TestRenderCLIFlag(t *testing.T) {
	require.Equal(t, "None", renderCLIFlag(namedOption{}))
	require.Equal(t, "None", renderCLIFlag(namedOption{docOption: docOption{CLIFlag: "None"}}))
	require.Equal(t, "`--dry-run`", renderCLIFlag(namedOption{docOption: docOption{CLIFlag: "--dry-run"}}))
}
