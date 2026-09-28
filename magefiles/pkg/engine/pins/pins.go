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

// Package pins is the thirdparty-src/pins.json manifest: which upstream repositories this repo
// pulls source from, which files, and at which tags.
//
// It is the data layer only -- reading and rewriting the manifest, the tag arithmetic that maps a
// tag to its minor line, and the git ls-remote query that finds the newest tag on a line. Acting
// on it, by pulling the source those pins name, is magefiles/pkg/engine/source. See
// docs/dev/thirdparty-src.md.
package pins

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Dir is the root the pinned upstream source is pulled into and generated from.
const Dir = "thirdparty-src"

// Pull describes one upstream file set to pull verbatim into thirdparty-src/ for
// pkg/engineconfig/extract to statically parse. See docs/dev/thirdparty-src.md.
type Pull struct {
	RepoURL string
	Tag     string
	DestDir string
	Files   []string
}

// Pins is the on-disk form of thirdparty-src/pins.json, the single source of truth
// for which upstream files this repo pulls and at which tags.
type Pins struct {
	Distros []Distro `json:"distros"`
}

// Distro pins one upstream repo. Every tag pulls the same file set into
// thirdparty-src/<name>/<minor>, where <minor> is derived from the tag (v1.35.8+k3s1 -> v1_35).
type Distro struct {
	Name  string   `json:"name"`
	Repo  string   `json:"repo"`
	Files []string `json:"files"`
	Tags  []string `json:"tags"`
}

// Path is the manifest itself: which tags are pinned, and where each distro's source comes
// from.
const Path = "thirdparty-src/pins.json"

var tagVersionPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)`)

// Read loads and parses thirdparty-src/pins.json.
func Read() (Pins, error) {
	var pins Pins

	data, err := os.ReadFile(Path)
	if err != nil {
		return pins, fmt.Errorf("reading %s: %w", Path, err)
	}
	if err := json.Unmarshal(data, &pins); err != nil {
		return pins, fmt.Errorf("parsing %s: %w", Path, err)
	}
	return pins, nil
}

// Write rewrites thirdparty-src/pins.json from the in-memory pins.
func (p Pins) Write() error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", Path, err)
	}
	return os.WriteFile(Path, append(data, '\n'), 0o644)
}

// Distro returns the pinned entry for a distro name.
func (p *Pins) Distro(name string) (*Distro, error) {
	var known []string
	for i := range p.Distros {
		if p.Distros[i].Name == name {
			return &p.Distros[i], nil
		}
		known = append(known, p.Distros[i].Name)
	}
	return nil, fmt.Errorf("unknown distro %q, expected one of: %s", name, strings.Join(known, ", "))
}

// Pulls flattens the pins into one pull per pinned tag.
func (p Pins) Pulls() ([]Pull, error) {
	var pulls []Pull
	for _, d := range p.Distros {
		for _, tag := range d.Tags {
			pull, err := d.Pull(tag)
			if err != nil {
				return nil, err
			}
			pulls = append(pulls, pull)
		}
	}
	return pulls, nil
}

// Pull describes what pulling one of this distro's tags copies, and to where.
func (d Distro) Pull(tag string) (Pull, error) {
	minor, err := TagMinor(tag)
	if err != nil {
		return Pull{}, fmt.Errorf("%s: %s: %w", Path, d.Name, err)
	}
	return Pull{
		RepoURL: d.Repo,
		Tag:     tag,
		DestDir: filepath.Join(Dir, d.Name, minor),
		Files:   d.Files,
	}, nil
}

// SetTag pins tag for its minor line, replacing whatever that line held. It reports the tag
// previously pinned there ("" when the minor line is new) and whether anything changed.
func (d *Distro) SetTag(tag string) (prev string, changed bool, err error) {
	minor, err := TagMinor(tag)
	if err != nil {
		return "", false, err
	}

	for i, existing := range d.Tags {
		existingMinor, err := TagMinor(existing)
		if err != nil {
			return "", false, fmt.Errorf("%s: %s: %w", Path, d.Name, err)
		}
		if existingMinor != minor {
			continue
		}
		if existing == tag {
			return existing, false, nil
		}
		d.Tags[i] = tag
		return existing, true, nil
	}

	d.Tags = append(d.Tags, tag)
	return "", true, d.SortTags()
}

// SortTags orders the pinned tags newest first, so a freshly pinned minor line lands where a
// reader expects it rather than at the end of the list.
func (d *Distro) SortTags() error {
	var sortErr error
	slices.SortFunc(d.Tags, func(a, b string) int {
		va, err := TagVersion(a)
		if err != nil {
			sortErr = err
		}
		vb, err := TagVersion(b)
		if err != nil {
			sortErr = err
		}
		return slices.Compare(vb[:], va[:])
	})
	return sortErr
}

// SortDesc orders an arbitrary list of tags newest first, so generated output and the lines it
// prints read the way a reader scanning for the current release expects.
//
// Distro.SortTags is the same ordering over one distro's pinned tags, where a minor line holds
// exactly one tag; this one also has to break the tie two tags of the same version produce --
// the rke2rN or k3sN revision -- which pins.json never contains.
func SortDesc(tags []string) error {
	var sortErr error
	slices.SortFunc(tags, func(a, b string) int {
		va, err := TagVersion(a)
		if err != nil {
			sortErr = err
		}
		vb, err := TagVersion(b)
		if err != nil {
			sortErr = err
		}
		if c := slices.Compare(vb[:], va[:]); c != 0 {
			return c
		}
		return strings.Compare(b, a) // same version, order by the rke2rN/k3sN revision
	})
	return sortErr
}

// TagVersion parses the leading major.minor.patch of an upstream tag ("v1.35.8+k3s1").
func TagVersion(tag string) ([3]int, error) {
	m := tagVersionPattern.FindStringSubmatch(tag)
	if m == nil {
		return [3]int{}, fmt.Errorf("tag %q does not start with vMAJOR.MINOR.PATCH", tag)
	}

	var v [3]int
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return [3]int{}, fmt.Errorf("tag %q: %w", tag, err)
		}
		v[i] = n
	}
	return v, nil
}

// TagMinor is the thirdparty-src directory name for a tag's minor line ("v1.35.8+k3s1" -> "v1_35").
func TagMinor(tag string) (string, error) {
	v, err := TagVersion(tag)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("v%d_%d", v[0], v[1]), nil
}

// RemoteTags lists every non-RC upstream tag on a minor line ("v1.36"), in the order git
// ls-remote returned them. Touches the network.
func RemoteTags(repoURL, prefix string) ([]string, error) {
	lsRemoteCmd := exec.Command("git", "ls-remote", "--tags", "--refs", repoURL)
	out, err := lsRemoteCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-remote: %w", err)
	}

	var tags []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		_, tag, ok := strings.Cut(line, "refs/tags/")
		if !ok || !strings.HasPrefix(tag, prefix+".") {
			continue
		}
		if strings.Contains(strings.ToLower(tag), "rc") {
			continue
		}
		if _, err := TagVersion(tag); err != nil {
			continue
		}
		tags = append(tags, tag)
	}

	if len(tags) == 0 {
		return nil, fmt.Errorf("no non-RC tag matching prefix %q found for %s", prefix, repoURL)
	}
	return tags, nil
}

// LatestTag is the highest-versioned of those tags. Ties on version (the same patch
// released twice, v1.36.1+rke2r1 and +rke2r2) keep the first one ls-remote listed.
func LatestTag(repoURL, prefix string) (string, error) {
	tags, err := RemoteTags(repoURL, prefix)
	if err != nil {
		return "", err
	}

	var best string
	var bestVer [3]int
	for _, tag := range tags {
		ver, err := TagVersion(tag)
		if err != nil {
			continue
		}
		if best == "" || slices.Compare(ver[:], bestVer[:]) > 0 {
			best = tag
			bestVer = ver
		}
	}
	return best, nil
}
