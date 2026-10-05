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

package distrocfg

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
)

// ManagedDir is a directory cargoship prunes, and how much of it it is allowed to prune.
type ManagedDir struct {
	// Path is the directory on the host.
	Path string
	// Glob limits pruning to the file names it matches, for a directory cargoship shares with
	// something else. An empty Glob means every file in the directory is cargoship's.
	//
	// The engine's manifest directory is the case this exists for: cargoship writes the
	// HelmChartConfig files in it, while the engine ships its own charts there, and removing
	// those would take the cluster apart.
	Glob string
}

// prunes reports whether a file in the directory is one cargoship may remove.
func (d ManagedDir) prunes(path string) bool {
	if d.Glob == "" {
		return true
	}
	ok, err := filepath.Match(d.Glob, filepath.Base(path))
	// A malformed pattern matches nothing, which leaves the file alone. Pruning is cleanup,
	// and cleanup is not worth deleting something on a guess.
	return err == nil && ok
}

// StaleFiles returns the files under dirs that desired no longer names, sorted by path.
//
// A managed directory holds only files cargoship put there, so a file in one that is not in the
// desired set was written for something the cluster configuration has since dropped -- a
// registry that no longer has an inline CA, say. Such a file is harmless while it sits there,
// since nothing references it, but it also never goes away on its own, and a certificate left
// behind on every node long after the registry is gone is the kind of thing that turns up in an
// audit rather than in a log.
//
// A directory that does not exist, or that cannot be listed, contributes nothing: pruning is
// cleanup, and there is no point failing an apply over it.
func StaleFiles(h *cluster.ZarfHost, dirs []ManagedDir, desired map[string]DesiredFile) []string {
	var stale []string
	for _, dir := range dirs {
		for _, path := range listFiles(h, dir.Path) {
			if _, ok := desired[path]; ok {
				continue
			}
			if dir.prunes(path) {
				stale = append(stale, path)
			}
		}
	}
	sort.Strings(stale)
	return stale
}

// RemoveStaleFiles deletes the files StaleFiles finds. It reports the first deletion error, so a
// file that cannot be removed is surfaced rather than left to be rediscovered on every run.
func RemoveStaleFiles(h *cluster.ZarfHost, dirs []ManagedDir, desired map[string]DesiredFile) error {
	for _, path := range StaleFiles(h, dirs, desired) {
		if err := h.DeleteFile(path); err != nil {
			return fmt.Errorf("removing stale file %s: %w", path, err)
		}
	}
	return nil
}

// listFiles is the directory listing StaleFiles works from. It is a variable so that tests can
// supply a listing without a host to run find on.
var listFiles = listManagedFiles

// listManagedFiles returns the files directly under dir on the host. Subdirectories are not
// descended into and are not returned: cargoship writes flat directories, and anything nested is
// something else's.
func listManagedFiles(h *cluster.ZarfHost, dir string) []string {
	if dir == "" || !h.FileExist(dir) {
		return nil
	}
	out, err := h.SudoExecOutput(fmt.Sprintf("find %s -maxdepth 1 -type f", dir))
	if err != nil {
		return nil
	}

	var files []string
	for _, line := range strings.Split(out, "\n") {
		if path := strings.TrimSpace(line); path != "" {
			files = append(files, path)
		}
	}
	return files
}
