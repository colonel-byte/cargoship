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

package phasedoc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/magefiles/pkg/repo"
	"github.com/stretchr/testify/require"
)

// TestDocsCoverEveryPage pins the set of actions that get a page. A page is only generated for an
// entry here, so dropping one silently removes it from the book.
func TestDocsCoverEveryPage(t *testing.T) {
	docs := Docs()

	names := make([]string, 0, len(docs))
	for _, d := range docs {
		names = append(names, d.Name)
		require.NotEmpty(t, d.Phases, "%s has no phases to document", d.Name)
	}

	require.ElementsMatch(t,
		[]string{"apply", "reset", "kube-config", "prepare", "engine-config-sync"},
		names)
}

// TestDryRunMatchesTheCheckedInPages ties each entry's DryRun to what its page actually says. The
// flag decides whether the page describes --dry-run at all, so an entry that disagrees with the
// command it documents produces a page describing a flag the command does not accept, or omitting
// one it does -- neither of which fails anything on its own.
func TestDryRunMatchesTheCheckedInPages(t *testing.T) {
	for _, d := range Docs() {
		t.Run(d.Name, func(t *testing.T) {
			page := renderToString(t, d)

			if d.DryRun {
				require.Contains(t, page, "--dry-run")
			} else {
				require.NotContains(t, page, "--dry-run")
			}
		})
	}
}

// TestWriteNamesThePageAfterTheAction checks the filename, which is what docs/SUMMARY.md links to.
func TestWriteNamesThePageAfterTheAction(t *testing.T) {
	require.Equal(t, "docs/phases", Dir)
	repo.Chdir(t)

	docs := Docs()
	require.NotEmpty(t, docs)

	for _, d := range docs {
		require.FileExists(t, filepath.Join(Dir, d.Name+".md"),
			"the checked-in page for %s", d.Name)
	}
}

// TestEveryPhaseIsDescribed guards the one way a page can be silently incomplete: a phase with no
// explanation renders as a bare title, which reads as a finished list item.
func TestEveryPhaseIsDescribed(t *testing.T) {
	for _, d := range Docs() {
		t.Run(d.Name, func(t *testing.T) {
			for _, p := range d.Phases {
				require.NotEmpty(t, strings.TrimSpace(p.Explanation()),
					"phase %q in %s has no explanation", p.Title(), d.Name)
				require.NotEmpty(t, strings.TrimSpace(p.Title()),
					"a phase in %s has no title", d.Name)
			}
		})
	}
}

// renderToString writes one doc through Write and reads it back, by pointing Dir's join at a
// temporary directory. Write builds its path from Dir, so the test chdirs rather than reaching
// into the package's internals.
func renderToString(t *testing.T, d Doc) string {
	t.Helper()

	dir := t.TempDir()
	restore, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, Dir), 0o775))
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(restore)) })

	require.NoError(t, Write(d))

	content, err := os.ReadFile(filepath.Join(Dir, d.Name+".md"))
	require.NoError(t, err)
	return string(content)
}
