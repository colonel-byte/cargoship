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
	"strings"
)

// PullEngineSource fetches pinned k3s/RKE2 source files
//
// Fetches the raw files pinned in thirdparty-src/pins.json at their exact tags and
// commits them under thirdparty-src/ as plain text -- never as a Go module dependency
// (their go.mod replace directives make that unsafe). This is the only step in the
// engine-config codegen pipeline that touches the network; Generate.EngineConfig runs
// fully offline against what this writes.
func (Generate) PullEngineSource() error {
	pins, err := readEnginePins()
	if err != nil {
		return err
	}

	pulls, err := pins.pulls()
	if err != nil {
		return err
	}

	for _, p := range pulls {
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
