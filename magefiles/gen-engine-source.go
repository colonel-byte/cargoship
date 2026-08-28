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

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// engineSourcePull describes one upstream file set to pull verbatim into thirdparty-src/ for
// src/pkg/engineconfig/extract to statically parse. See docs/dev/thirdparty-src.md.
type engineSourcePull struct {
	repoURL string
	tag     string
	destDir string
	files   []string
}

const (
	k3sUrl  = "https://github.com/k3s-io/k3s"
	rke2Url = "https://github.com/rancher/rke2"
)

var (
	rke2Files = []string{
		"pkg/cli/cmds/server.go",
		"pkg/cli/cmds/agent.go",
		"pkg/cli/cmds/root.go",
		"pkg/cli/cmds/k3sopts.go",
	}
	k3sFiles = []string{
		"pkg/cli/cmds/server.go",
		"pkg/cli/cmds/agent.go",
	}
)

var engineSourceRepos = map[string]string{
	"k3s":  k3sUrl,
	"rke2": rke2Url,
}

var tagVersionPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)`)

var engineSourcePulls = []engineSourcePull{
	{
		repoURL: k3sUrl,
		tag:     "v1.36.4+k3s1",
		destDir: "thirdparty-src/k3s/v1_36",
		files:   k3sFiles,
	},
	{
		repoURL: k3sUrl,
		tag:     "v1.35.8+k3s1",
		destDir: "thirdparty-src/k3s/v1_35",
		files:   k3sFiles,
	},
	{
		repoURL: k3sUrl,
		tag:     "v1.34.11+k3s1",
		destDir: "thirdparty-src/k3s/v1_34",
		files:   k3sFiles,
	},
	{
		repoURL: k3sUrl,
		tag:     "v1.33.13+k3s1",
		destDir: "thirdparty-src/k3s/v1_33",
		files:   k3sFiles,
	},
	{
		repoURL: k3sUrl,
		tag:     "v1.32.13+k3s1",
		destDir: "thirdparty-src/k3s/v1_32",
		files:   k3sFiles,
	},
	{
		repoURL: k3sUrl,
		tag:     "v1.31.14+k3s1",
		destDir: "thirdparty-src/k3s/v1_31",
		files:   k3sFiles,
	},
	{
		repoURL: rke2Url,
		tag:     "v1.36.3+rke2r1",
		destDir: "thirdparty-src/rke2/v1_36",
		files:   rke2Files,
	},
	{
		repoURL: rke2Url,
		tag:     "v1.35.7+rke2r1",
		destDir: "thirdparty-src/rke2/v1_35",
		files:   rke2Files,
	},
	{
		repoURL: rke2Url,
		tag:     "v1.34.10+rke2r1",
		destDir: "thirdparty-src/rke2/v1_34",
		files:   rke2Files,
	},
	{
		repoURL: rke2Url,
		tag:     "v1.33.13+rke2r1",
		destDir: "thirdparty-src/rke2/v1_33",
		files:   rke2Files,
	},
	{
		repoURL: rke2Url,
		tag:     "v1.32.13+rke2r1",
		destDir: "thirdparty-src/rke2/v1_32",
		files:   rke2Files,
	},
	{
		repoURL: rke2Url,
		tag:     "v1.31.14+rke2r1",
		destDir: "thirdparty-src/rke2/v1_31",
		files:   rke2Files,
	},
}

// PullEngineSource fetches pinned k3s/RKE2 source files
//
// Fetches the raw files listed in engineSourcePulls at their exact pinned tags and
// commits them under thirdparty-src/ as plain text -- never as a Go module dependency
// (their go.mod replace directives make that unsafe). This is the only step in the
// engine-config codegen pipeline that touches the network; Generate.EngineConfig runs
// fully offline against what this writes.
func (Generate) PullEngineSource() error {
	for _, p := range engineSourcePulls {
		if err := pullEngineSource(p); err != nil {
			return fmt.Errorf("pulling %s@%s: %w", p.repoURL, p.tag, err)
		}
	}
	return nil
}

func pullEngineSource(p engineSourcePull) error {
	tmp, err := os.MkdirTemp("", "engine-src-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	srcDir := filepath.Join(tmp, "src")
	cloneCmd := exec.Command("git", "clone", "--quiet", "--depth", "1", "--branch", p.tag, p.repoURL, srcDir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone: %w: %s", err, out)
	}

	revParseCmd := exec.Command("git", "-C", srcDir, "rev-parse", "HEAD")
	commitOut, err := revParseCmd.Output()
	if err != nil {
		return fmt.Errorf("git rev-parse: %w", err)
	}
	commit := strings.TrimSpace(string(commitOut))

	if err := os.MkdirAll(p.destDir, 0o755); err != nil {
		return err
	}
	for _, f := range p.files {
		data, err := os.ReadFile(filepath.Join(srcDir, f))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(p.destDir, "zz_"+filepath.Base(f)), data, 0o644); err != nil {
			return err
		}
	}

	sourceTxt := fmt.Sprintf("repo:   %s\ntag:    %s\ncommit: %s\nfiles:  %s\n",
		p.repoURL, p.tag, commit, strings.Join(p.files, " "))
	if err := os.WriteFile(filepath.Join(p.destDir, "SOURCE.txt"), []byte(sourceTxt), 0o644); err != nil {
		return err
	}

	fmt.Printf("Pulled %d file(s) from %s@%s into %s\n", len(p.files), p.repoURL, p.tag, p.destDir)
	return nil
}

// LatestTag prints the newest non-RC tag for a distro ("k3s" or "rke2") matching a
// major.minor prefix (e.g. "v1.35"), for pinning new engineSourcePulls entries in this file.
//
//	mage generate:latestTag rke2 v1.35
func (Generate) LatestTag(distro, prefix string) error {
	repoURL, ok := engineSourceRepos[distro]
	if !ok {
		return fmt.Errorf("unknown distro %q, expected one of: k3s, rke2", distro)
	}

	tag, err := latestTag(repoURL, prefix)
	if err != nil {
		return err
	}

	fmt.Println(tag)
	return nil
}

func latestTag(repoURL, prefix string) (string, error) {
	lsRemoteCmd := exec.Command("git", "ls-remote", "--tags", "--refs", repoURL)
	out, err := lsRemoteCmd.Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote: %w", err)
	}

	var best string
	var bestVer [3]int
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

		m := tagVersionPattern.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		patch, _ := strconv.Atoi(m[3])
		ver := [3]int{major, minor, patch}

		if best == "" || ver[0] > bestVer[0] ||
			(ver[0] == bestVer[0] && ver[1] > bestVer[1]) ||
			(ver[0] == bestVer[0] && ver[1] == bestVer[1] && ver[2] > bestVer[2]) {
			best = tag
			bestVer = ver
		}
	}

	if best == "" {
		return "", fmt.Errorf("no non-RC tag matching prefix %q found for %s", prefix, repoURL)
	}
	return best, nil
}
