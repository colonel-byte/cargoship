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

// Package source syncs thirdparty-src/ with the manifest in magefiles/pkg/engine/pins.
//
// It fetches the raw upstream files each pin names, at their exact tags, and writes them out as
// plain text -- never as a Go module dependency, because the k3s and RKE2 go.mod replace
// directives make that unsafe. This is the only step in the engine-config codegen pipeline that
// touches the network; the generation that follows runs fully offline against what this writes.
// See docs/dev/thirdparty-src.md.
package source

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/colonel-byte/cargoship/magefiles/pkg/engine/pins"
)

// PullAll fetches every file set pinned in thirdparty-src/pins.json at its pinned tag.
func PullAll() error {
	manifest, err := pins.Read()
	if err != nil {
		return err
	}

	pulls, err := manifest.Pulls()
	if err != nil {
		return err
	}

	for _, p := range pulls {
		if err := Pull(p); err != nil {
			return fmt.Errorf("pulling %s@%s: %w", p.RepoURL, p.Tag, err)
		}
	}
	return nil
}

// Pull clones one pinned tag and copies its files into the pull's destination, alongside a
// SOURCE.txt naming the repository, tag and commit they came from.
func Pull(p pins.Pull) error {
	tmp, err := os.MkdirTemp("", "engine-src-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck // a scratch clone left behind is not worth failing a pull over

	srcDir := filepath.Join(tmp, "src")
	cloneCmd := exec.Command("git", "clone", "--quiet", "--depth", "1", "--branch", p.Tag, p.RepoURL, srcDir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone: %w: %s", err, out)
	}

	revParseCmd := exec.Command("git", "-C", srcDir, "rev-parse", "HEAD")
	commitOut, err := revParseCmd.Output()
	if err != nil {
		return fmt.Errorf("git rev-parse: %w", err)
	}
	commit := strings.TrimSpace(string(commitOut))

	if err := os.MkdirAll(p.DestDir, 0o755); err != nil {
		return err
	}
	for _, f := range p.Files {
		data, err := os.ReadFile(filepath.Join(srcDir, f))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(p.DestDir, "zz_"+filepath.Base(f)), data, 0o644); err != nil {
			return err
		}
	}

	sourceTxt := fmt.Sprintf("repo:   %s\ntag:    %s\ncommit: %s\nfiles:  %s\n",
		p.RepoURL, p.Tag, commit, strings.Join(p.Files, " "))
	if err := os.WriteFile(filepath.Join(p.DestDir, "SOURCE.txt"), []byte(sourceTxt), 0o644); err != nil {
		return err
	}

	fmt.Printf("Pulled %d file(s) from %s@%s into %s\n", len(p.Files), p.RepoURL, p.Tag, p.DestDir)
	return nil
}

// PinLatest resolves the newest non-RC tag matching prefix on a distro's repository, pins it in
// thirdparty-src/pins.json, and re-pulls that version's source when the pin moved. It returns the
// resolved tag whether or not anything changed.
func PinLatest(manifest *pins.Pins, distro, prefix string) (string, error) {
	d, err := manifest.Distro(distro)
	if err != nil {
		return "", err
	}

	tag, err := pins.LatestTag(d.Repo, prefix)
	if err != nil {
		return "", err
	}

	prev, changed, err := d.SetTag(tag)
	if err != nil {
		return "", err
	}

	minor, err := pins.TagMinor(tag)
	if err != nil {
		return "", err
	}
	switch {
	case !changed:
		fmt.Printf("%s: %s %s unchanged\n", pins.Path, distro, minor)
		return tag, nil
	case prev == "":
		fmt.Printf("%s: %s %s added at %s\n", pins.Path, distro, minor, tag)
	default:
		fmt.Printf("%s: %s %s %s -> %s\n", pins.Path, distro, minor, prev, tag)
	}

	if err := manifest.Write(); err != nil {
		return "", err
	}

	pull, err := d.Pull(tag)
	if err != nil {
		return "", err
	}
	if err := Pull(pull); err != nil {
		return "", fmt.Errorf("pulling %s@%s: %w", pull.RepoURL, pull.Tag, err)
	}
	return tag, nil
}

// LatestTag resolves and pins one minor line, printing the tag it settled on.
func LatestTag(distro, prefix string) (string, error) {
	manifest, err := pins.Read()
	if err != nil {
		return "", err
	}
	return PinLatest(&manifest, distro, prefix)
}

// UpdatePins runs the same resolve-pin-pull cycle over every minor line already in
// thirdparty-src/pins.json, so a routine "are we behind upstream?" pass is one command rather
// than twelve.
func UpdatePins() error {
	manifest, err := pins.Read()
	if err != nil {
		return err
	}

	// Snapshot the pinned minor lines first: PinLatest rewrites Tags as it goes.
	type pinnedLine struct{ distro, prefix string }
	var lines []pinnedLine
	for _, d := range manifest.Distros {
		for _, tag := range d.Tags {
			v, err := pins.TagVersion(tag)
			if err != nil {
				return fmt.Errorf("%s: %s: %w", pins.Path, d.Name, err)
			}
			lines = append(lines, pinnedLine{d.Name, fmt.Sprintf("v%d.%d", v[0], v[1])})
		}
	}

	for _, l := range lines {
		if _, err := PinLatest(&manifest, l.distro, l.prefix); err != nil {
			return err
		}
	}

	fmt.Println("Run mage generate:engineConfig to regenerate structs for any version that moved.")
	return nil
}
