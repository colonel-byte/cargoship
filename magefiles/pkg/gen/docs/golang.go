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

package docs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// golangDocsRoots are the module directories documented under golangDocsDir: the package's public
// surface. cmd/ is the CLI already covered by docs/commands, internal/ is not importable outside
// the module, and magefiles/ is build tooling rather than library code.
var golangDocsRoots = []string{"pkg", "api", "types"}

// generateGolangDocs renders docs/golang/*.md with gomarkdoc, one page per package directory found
// under golangDocsRoots.
//
// gomarkdoc is invoked once with every package directory listed explicitly, rather than with a
// "/..." pattern, because "/..." makes {{.Dir}} resolve to "" for each pattern's own root package
// and gomarkdoc writes that page as "<dir>/.md" -- a name getMarkdownTree below cannot sort next to
// the packages one level under it.
func generateGolangDocs() error {
	dirs, err := golangPackageDirs()
	if err != nil {
		return err
	}
	if len(dirs) == 0 {
		return nil
	}

	args := append([]string{"tool", "gomarkdoc", "--output", golangDocsDir + "/{{.Dir}}.md"}, dirs...)
	cmd := exec.Command("go", args...)
	out, err := cmd.CombinedOutput()
	fmt.Print(string(out))
	if err != nil {
		return fmt.Errorf("gomarkdoc: %w", err)
	}
	return nil
}

// golangPackageDirs lists every directory under golangDocsRoots that holds at least one .go file,
// sorted and prefixed with "./" the way gomarkdoc expects a package argument.
func golangPackageDirs() ([]string, error) {
	found := map[string]bool{}
	for _, root := range golangDocsRoots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Ext(path) != ".go" {
				return nil
			}
			found[filepath.Dir(path)] = true
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	dirs := make([]string, 0, len(found))
	for dir := range found {
		dirs = append(dirs, "./"+dir)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// golangSummary lists the Golang chapter's entries, nesting each package under whichever of its
// parent directories has a page of its own.
//
// A directory with no .go file directly in it -- pkg/ itself, or pkg/engineconfig, which holds
// only the extract and gen subpackages -- gets no page, so indenting by raw path depth would nest
// its children under whatever sibling entry happened to precede them. Indenting instead by the
// count of ancestor directories that do have a page keeps an undocumented directory's children at
// the level of its nearest documented ancestor, or top-level if it has none.
//
// Sorting is done on the page path with its .md suffix stripped, not on the raw filesystem walk
// order: a directory entry "coci" and a file "coci.md" would otherwise sort as
// "coci" < "coci.md", listing pkg/coci's subpackages before pkg/coci's own page. Stripping the
// suffix first makes "pkg/coci" a proper string prefix of "pkg/coci/sub", which sorts before it.
func golangSummary() ([]string, error) {
	var keys []string
	err := filepath.WalkDir(golangDocsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(golangDocsDir, path)
		if err != nil {
			return err
		}
		keys = append(keys, strings.TrimSuffix(rel, ".md"))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)

	documented := make(map[string]bool, len(keys))
	for _, key := range keys {
		documented[key] = true
	}

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		segments := strings.Split(key, "/")
		depth := 0
		for i := 1; i < len(segments); i++ {
			if documented[strings.Join(segments[:i], "/")] {
				depth++
			}
		}
		indent := strings.Repeat("  ", depth)
		lines = append(lines, fmt.Sprintf("%s- [%s](golang/%s.md)", indent, filepath.Base(key), key))
	}
	return lines, nil
}
