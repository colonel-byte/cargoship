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

// Package engineconfig manages upstream k3s/RKE2 source pins, source pulls, and config struct codegen.
package engineconfig

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

// ThirdpartySrcDir is the root the pinned upstream source is pulled into and generated from.
const ThirdpartySrcDir = "thirdparty-src"

// EnginePinsPath is the canonical manifest of pinned upstream distros and tags.
const EnginePinsPath = "thirdparty-src/pins.json"

// SourcePull describes one upstream file set to pull verbatim into thirdparty-src/.
type SourcePull struct {
	RepoURL string
	Tag     string
	DestDir string
	Files   []string
}

// EnginePins is the on-disk form of thirdparty-src/pins.json.
type EnginePins struct {
	Distros []PinDistro `json:"distros"`
}

// PinDistro pins one upstream repository.
type PinDistro struct {
	Name  string   `json:"name"`
	Repo  string   `json:"repo"`
	Files []string `json:"files"`
	Tags  []string `json:"tags"`
}

var tagVersionPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)`)

// ReadEnginePins loads and parses thirdparty-src/pins.json.
func ReadEnginePins() (EnginePins, error) {
	var pins EnginePins

	data, err := os.ReadFile(EnginePinsPath)
	if err != nil {
		return pins, fmt.Errorf("reading %s: %w", EnginePinsPath, err)
	}
	if err := json.Unmarshal(data, &pins); err != nil {
		return pins, fmt.Errorf("parsing %s: %w", EnginePinsPath, err)
	}
	return pins, nil
}

// Write rewrites thirdparty-src/pins.json from the in-memory pins.
func (p EnginePins) Write() error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", EnginePinsPath, err)
	}
	return os.WriteFile(EnginePinsPath, append(data, '\n'), 0o644)
}

// Distro returns the pinned entry for a distro name.
func (p *EnginePins) Distro(name string) (*PinDistro, error) {
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
func (p EnginePins) Pulls() ([]SourcePull, error) {
	var pulls []SourcePull
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

func (d PinDistro) pull(tag string) (SourcePull, error) {
	minor, err := TagMinor(tag)
	if err != nil {
		return SourcePull{}, fmt.Errorf("%s: %s: %w", EnginePinsPath, d.Name, err)
	}
	return SourcePull{
		RepoURL: d.Repo,
		Tag:     tag,
		DestDir: filepath.Join(ThirdpartySrcDir, d.Name, minor),
		Files:   d.Files,
	}, nil
}

// SetTag pins tag for its minor line, replacing whatever that line held.
func (d *PinDistro) SetTag(tag string) (prev string, changed bool, err error) {
	minor, err := TagMinor(tag)
	if err != nil {
		return "", false, err
	}

	for i, existing := range d.Tags {
		existingMinor, err := TagMinor(existing)
		if err != nil {
			return "", false, fmt.Errorf("%s: %s: %w", EnginePinsPath, d.Name, err)
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

func (d *PinDistro) sortTags() error {
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

// TagMinor returns the thirdparty-src directory name for a tag's minor line ("v1.35.8+k3s1" -> "v1_35").
func TagMinor(tag string) (string, error) {
	v, err := TagVersion(tag)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("v%d_%d", v[0], v[1]), nil
}

// PinTag resolves the newest non-RC tag matching prefix, pins it in thirdparty-src/pins.json,
// and re-pulls that version's source when the pin moved. Returns the resolved tag.
func PinTag(pins *EnginePins, distro, prefix string) (string, error) {
	d, err := pins.Distro(distro)
	if err != nil {
		return "", err
	}

	tag, err := LatestTag(d.Repo, prefix)
	if err != nil {
		return "", err
	}

	prev, changed, err := d.SetTag(tag)
	if err != nil {
		return "", err
	}

	minor, err := TagMinor(tag)
	if err != nil {
		return "", err
	}
	switch {
	case !changed:
		fmt.Printf("%s: %s %s unchanged\n", EnginePinsPath, distro, minor)
		return tag, nil
	case prev == "":
		fmt.Printf("%s: %s %s added at %s\n", EnginePinsPath, distro, minor, tag)
	default:
		fmt.Printf("%s: %s %s %s -> %s\n", EnginePinsPath, distro, minor, prev, tag)
	}

	if err := pins.Write(); err != nil {
		return "", err
	}

	pull, err := d.pull(tag)
	if err != nil {
		return "", err
	}
	if err := PullEngineSource(pull); err != nil {
		return "", fmt.Errorf("pulling %s@%s: %w", pull.RepoURL, pull.Tag, err)
	}
	return tag, nil
}

// RemoteTags lists every non-RC upstream tag on a minor line ("v1.36").
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

// LatestTag is the highest-versioned of those tags.
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

// PullEngineSource pulls the specified source files into DestDir.
func PullEngineSource(p SourcePull) error {
	tmp, err := os.MkdirTemp("", "engine-src-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck // best-effort cleanup of a temp file the rename usually consumed

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

// PullAllEngineSources pulls all files pinned in thirdparty-src/pins.json.
func PullAllEngineSources() error {
	pins, err := ReadEnginePins()
	if err != nil {
		return err
	}

	pulls, err := pins.Pulls()
	if err != nil {
		return err
	}

	for _, p := range pulls {
		if err := PullEngineSource(p); err != nil {
			return fmt.Errorf("pulling %s@%s: %w", p.RepoURL, p.Tag, err)
		}
	}
	return nil
}

// UpdateAllPins refreshes every pinned minor line in thirdparty-src/pins.json.
func UpdateAllPins() error {
	pins, err := ReadEnginePins()
	if err != nil {
		return err
	}

	type pinnedLine struct{ distro, prefix string }
	var lines []pinnedLine
	for _, d := range pins.Distros {
		for _, tag := range d.Tags {
			v, err := TagVersion(tag)
			if err != nil {
				return fmt.Errorf("%s: %s: %w", EnginePinsPath, d.Name, err)
			}
			lines = append(lines, pinnedLine{
				distro: d.Name,
				prefix: fmt.Sprintf("v%d.%d", v[0], v[1]),
			})
		}
	}

	for _, l := range lines {
		if _, err := PinTag(&pins, l.distro, l.prefix); err != nil {
			return err
		}
	}

	fmt.Println("Run mage generate:engineConfig to regenerate structs for any version that moved.")
	return nil
}
