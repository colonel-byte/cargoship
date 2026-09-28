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

// Package book copies the two hand-written pages that live outside docs/ into the book, rewriting
// the relative links that would otherwise break once the page is served from a different
// directory.
package book

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/page"
)

// Two pages of the book are written outside docs/, because a reader other than mdBook looks for
// them where they are: GitHub renders README.md as the repository's front page, and
// .github/SECURITY.md as its security policy, from the fixed paths it expects. Each is copied into
// docs/ rather than symlinked, because the two readers resolve a relative link against different
// directories -- GitHub against the file's own directory in the repository, mdBook against docs/,
// since book.toml sets src = "docs" and the page is served from the site root. No single relative
// path satisfies both.
var bookPages = []struct {
	// source is the hand-written file. It keeps links relative to its own directory in the
	// repository, which is what GitHub and an editor want.
	source string

	// page is the generated copy under docs/, with its links rewritten for the book.
	page string
}{
	{
		source: "README.md",
		page:   "docs/index.md",
	},
	{
		source: ".github/SECURITY.md",
		page:   "docs/security.md",
	},
}

// markdownLink matches the target of a Markdown inline link or image, with its optional title.
// Reference-style links and autolinks are not matched, and neither source uses either.
var markdownLink = regexp.MustCompile(`(!?\]\()([^)\s]+)((?:\s+"[^"]*")?\))`)

// Generate copies each file in bookPages into the book, rewriting the links that would otherwise
// break there.
func Generate() error {
	for _, p := range bookPages {
		if err := generateBookPage(p.source, p.page); err != nil {
			return err
		}
	}
	return nil
}

// generateBookPage writes one page of the book from a source outside docs/.
//
// A link into docs/ is rewritten relative to the generated page. Anything else relative fails the
// run naming the target, since the alternative is publishing a link that 404s on a page nobody
// thinks of as generated. Absolute URLs, anchors, and the README's badge images pass through
// untouched.
func generateBookPage(source, page string) error {
	content, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("unable to read %s: %w", source, err)
	}

	rewritten, err := rebaseContent(source, page, string(content))
	if err != nil {
		return err
	}

	// A page that was a symlink in an earlier tree is still one here, and writing through it
	// would write to the source instead.
	if err := os.Remove(page); err != nil && !os.IsNotExist(err) {
		return err
	}

	fmt.Println(page)
	return os.WriteFile(page, []byte(bannerPrefix+rewritten), 0o644)
}

// bannerPrefix is what every generated book page opens with.
const bannerPrefix = page.Banner + "\n\n"

// rebaseContent rewrites every link in one page's content, and reports the targets that will not
// resolve in the book. Kept apart from the file handling so the rule can be exercised on a string.
func rebaseContent(source, generated, content string) (string, error) {
	sourceDir, pageDir := filepath.Dir(source), filepath.Dir(generated)

	var unresolved []string
	rewritten := markdownLink.ReplaceAllStringFunc(content, func(match string) string {
		parts := markdownLink.FindStringSubmatch(match)
		open, target, tail := parts[1], parts[2], parts[3]

		rebased, ok := rebaseLink(sourceDir, pageDir, target)
		if !ok {
			unresolved = append(unresolved, target)
			return match
		}
		return open + rebased + tail
	})

	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		return "", fmt.Errorf(
			"%s links to %s, which will not resolve in %s: the book is built from %s/, so a relative link has to point at a file inside it. Move the target under %s/, or write the link as an absolute URL",
			source, strings.Join(unresolved, ", "), generated, page.DocsDir, page.DocsDir,
		)
	}

	return rewritten, nil
}

// rebaseLink rewrites one link target from the source page's directory to the generated page's,
// and reports whether the result resolves.
//
// A target that is not a relative path into the repository -- an absolute URL, a protocol-relative
// one, a bare fragment -- is left exactly as it is and reported fine, because its meaning does not
// depend on which directory the page sits in.
func rebaseLink(sourceDir, pageDir, target string) (string, bool) {
	switch {
	case strings.HasPrefix(target, "#"):
		// A fragment is resolved against the page itself either way.
		return target, true
	case strings.Contains(target, "://"), strings.HasPrefix(target, "//"), strings.HasPrefix(target, "mailto:"):
		// A scheme, or a protocol-relative URL, is not a path in this repository.
		return target, true
	case target == "", strings.HasPrefix(target, "/"):
		// An empty target, and one rooted at the server, resolve to neither base.
		return target, false
	}

	// The fragment travels with the link but is not part of the path being checked.
	path, fragment := target, ""
	if i := strings.Index(path, "#"); i >= 0 {
		path, fragment = path[:i], path[i:]
	}

	// What the link means on GitHub: a path relative to the repository root. A target that
	// climbs out of the repository lands outside docs/ and is refused below with the rest.
	repoPath := filepath.Join(sourceDir, path)
	if repoPath != page.DocsDir && !strings.HasPrefix(repoPath, page.DocsDir+string(filepath.Separator)) {
		return target, false
	}

	// mdBook rewrites a .md target to .html itself, so the extension is left alone and the file
	// is what gets checked.
	if _, err := os.Stat(repoPath); err != nil {
		return target, false
	}

	rebased, err := filepath.Rel(pageDir, repoPath)
	if err != nil {
		return target, false
	}
	return rebased + fragment, true
}
