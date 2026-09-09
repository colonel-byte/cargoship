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

// This file holds the shared thirdparty-src/pins.json manifest layer. The mage targets that
// read or rewrite it each live in their own gen-engine-*.go file.

package main

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

// thirdpartySrcDir is the root the pinned upstream source is pulled into and generated from.
const thirdpartySrcDir = "thirdparty-src"

// engineSourcePull describes one upstream file set to pull verbatim into thirdparty-src/ for
// src/pkg/engineconfig/extract to statically parse. See docs/dev/thirdparty-src.md.
type engineSourcePull struct {
	repoURL string
	tag     string
	destDir string
	files   []string
}

// enginePins is the on-disk form of thirdparty-src/pins.json, the single source of truth
// for which upstream files this repo pulls and at which tags.
type enginePins struct {
	Distros []enginePinDistro `json:"distros"`
}

// enginePinDistro pins one upstream repo. Every tag pulls the same file set into
// thirdparty-src/<name>/<minor>, where <minor> is derived from the tag (v1.35.8+k3s1 -> v1_35).
type enginePinDistro struct {
	Name  string   `json:"name"`
	Repo  string   `json:"repo"`
	Files []string `json:"files"`
	Tags  []string `json:"tags"`
}

const enginePinsPath = "thirdparty-src/pins.json"

var tagVersionPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)`)

// readEnginePins loads and parses thirdparty-src/pins.json.
func readEnginePins() (enginePins, error) {
	var pins enginePins

	data, err := os.ReadFile(enginePinsPath)
	if err != nil {
		return pins, fmt.Errorf("reading %s: %w", enginePinsPath, err)
	}
	if err := json.Unmarshal(data, &pins); err != nil {
		return pins, fmt.Errorf("parsing %s: %w", enginePinsPath, err)
	}
	return pins, nil
}

// write rewrites thirdparty-src/pins.json from the in-memory pins.
func (p enginePins) write() error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", enginePinsPath, err)
	}
	return os.WriteFile(enginePinsPath, append(data, '\n'), 0o644)
}

// distro returns the pinned entry for a distro name.
func (p *enginePins) distro(name string) (*enginePinDistro, error) {
	var known []string
	for i := range p.Distros {
		if p.Distros[i].Name == name {
			return &p.Distros[i], nil
		}
		known = append(known, p.Distros[i].Name)
	}
	return nil, fmt.Errorf("unknown distro %q, expected one of: %s", name, strings.Join(known, ", "))
}

// pulls flattens the pins into one pull per pinned tag.
func (p enginePins) pulls() ([]engineSourcePull, error) {
	var pulls []engineSourcePull
	for _, d := range p.Distros {
		for _, tag := range d.Tags {
			pull, err := d.pull(tag)
			if err != nil {
				return nil, err
			}
			pulls = append(pulls, pull)
		}
	}
	return pulls, nil
}

// pull describes what pulling one of this distro's tags copies, and to where.
func (d enginePinDistro) pull(tag string) (engineSourcePull, error) {
	minor, err := tagMinor(tag)
	if err != nil {
		return engineSourcePull{}, fmt.Errorf("%s: %s: %w", enginePinsPath, d.Name, err)
	}
	return engineSourcePull{
		repoURL: d.Repo,
		tag:     tag,
		destDir: filepath.Join(thirdpartySrcDir, d.Name, minor),
		files:   d.Files,
	}, nil
}

// setTag pins tag for its minor line, replacing whatever that line held. It reports the tag
// previously pinned there ("" when the minor line is new) and whether anything changed.
func (d *enginePinDistro) setTag(tag string) (prev string, changed bool, err error) {
	minor, err := tagMinor(tag)
	if err != nil {
		return "", false, err
	}

	for i, existing := range d.Tags {
		existingMinor, err := tagMinor(existing)
		if err != nil {
			return "", false, fmt.Errorf("%s: %s: %w", enginePinsPath, d.Name, err)
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
	return "", true, d.sortTags()
}

// sortTags orders the pinned tags newest first, so a freshly pinned minor line lands where a
// reader expects it rather than at the end of the list.
func (d *enginePinDistro) sortTags() error {
	var sortErr error
	slices.SortFunc(d.Tags, func(a, b string) int {
		va, err := tagVersion(a)
		if err != nil {
			sortErr = err
		}
		vb, err := tagVersion(b)
		if err != nil {
			sortErr = err
		}
		return slices.Compare(vb[:], va[:])
	})
	return sortErr
}

// tagVersion parses the leading major.minor.patch of an upstream tag ("v1.35.8+k3s1").
func tagVersion(tag string) ([3]int, error) {
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

// tagMinor is the thirdparty-src directory name for a tag's minor line ("v1.35.8+k3s1" -> "v1_35").
func tagMinor(tag string) (string, error) {
	v, err := tagVersion(tag)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("v%d_%d", v[0], v[1]), nil
}

// pinTag resolves the newest non-RC tag matching prefix, pins it in thirdparty-src/pins.json,
// and re-pulls that version's source when the pin moved. Returns the resolved tag.
func pinTag(pins *enginePins, distro, prefix string) (string, error) {
	d, err := pins.distro(distro)
	if err != nil {
		return "", err
	}

	tag, err := latestTag(d.Repo, prefix)
	if err != nil {
		return "", err
	}

	prev, changed, err := d.setTag(tag)
	if err != nil {
		return "", err
	}

	minor, err := tagMinor(tag)
	if err != nil {
		return "", err
	}
	switch {
	case !changed:
		fmt.Printf("%s: %s %s unchanged\n", enginePinsPath, distro, minor)
		return tag, nil
	case prev == "":
		fmt.Printf("%s: %s %s added at %s\n", enginePinsPath, distro, minor, tag)
	default:
		fmt.Printf("%s: %s %s %s -> %s\n", enginePinsPath, distro, minor, prev, tag)
	}

	if err := pins.write(); err != nil {
		return "", err
	}

	pull, err := d.pull(tag)
	if err != nil {
		return "", err
	}
	if err := pullEngineSource(pull); err != nil {
		return "", fmt.Errorf("pulling %s@%s: %w", pull.repoURL, pull.tag, err)
	}
	return tag, nil
}

// remoteTags lists every non-RC upstream tag on a minor line ("v1.36"), in the order git
// ls-remote returned them. Touches the network.
func remoteTags(repoURL, prefix string) ([]string, error) {
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
		if _, err := tagVersion(tag); err != nil {
			continue
		}
		tags = append(tags, tag)
	}

	if len(tags) == 0 {
		return nil, fmt.Errorf("no non-RC tag matching prefix %q found for %s", prefix, repoURL)
	}
	return tags, nil
}

// latestTag is the highest-versioned of those tags. Ties on version (the same patch
// released twice, v1.36.1+rke2r1 and +rke2r2) keep the first one ls-remote listed.
func latestTag(repoURL, prefix string) (string, error) {
	tags, err := remoteTags(repoURL, prefix)
	if err != nil {
		return "", err
	}

	var best string
	var bestVer [3]int
	for _, tag := range tags {
		ver, err := tagVersion(tag)
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
