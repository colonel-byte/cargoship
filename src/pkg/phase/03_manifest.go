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

package phase

import (
	"context"
	"strings"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	// manifestDir is the remote directory cargoship uses to track files it uploaded to a host,
	// kept outside the distro's own data/config directories so it survives long enough to drive
	// cleanup of paths an uninstall wouldn't otherwise know about (e.g. a binary install target).
	manifestDir = "/var/lib/cargoship"
	// manifestFile is the remote path of the upload manifest.
	manifestFile = manifestDir + "/manifest.txt"
	// manifestSep separates the category from the path on each manifest line.
	manifestSep = "\t"
)

// ManifestEntry is a single file cargoship uploaded to a host, recorded so it can be found
// and removed later during an upgrade or uninstall.
type ManifestEntry struct {
	// Category labels why the file was uploaded, e.g. "engine", "image", "file", "data".
	Category string
	// Path is the absolute path of the file on the remote host.
	Path string
}

// encodeManifest renders entries as manifest file content, one "category\tpath" line each.
func encodeManifest(entries []ManifestEntry) string {
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, e.Category+manifestSep+e.Path)
	}
	return strings.Join(lines, "\n") + "\n"
}

// parseManifest reads manifest file content back into entries. Malformed lines are skipped.
func parseManifest(content string) []ManifestEntry {
	entries := []ManifestEntry{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, manifestSep, 2)
		if len(parts) != 2 {
			continue
		}
		entries = append(entries, ManifestEntry{Category: parts[0], Path: parts[1]})
	}
	return entries
}

// mergeManifest dedupes entries by Path, keeping insertion order and the last Category seen
// for a given Path.
func mergeManifest(entries []ManifestEntry) []ManifestEntry {
	order := make([]string, 0, len(entries))
	byPath := make(map[string]ManifestEntry, len(entries))
	for _, e := range entries {
		if _, exists := byPath[e.Path]; !exists {
			order = append(order, e.Path)
		}
		byPath[e.Path] = e
	}
	merged := make([]ManifestEntry, 0, len(order))
	for _, path := range order {
		merged = append(merged, byPath[path])
	}
	return merged
}

// diffManifest returns entries present in old but missing from current, keyed by Path. This is
// how a stale file from a previous version (e.g. a renamed engine binary) is found after an
// upgrade uploads its replacement.
func diffManifest(old, current []ManifestEntry) []ManifestEntry {
	keep := make(map[string]bool, len(current))
	for _, e := range current {
		keep[e.Path] = true
	}
	stale := []ManifestEntry{}
	for _, e := range old {
		if !keep[e.Path] {
			stale = append(stale, e)
		}
	}
	return stale
}

// filterManifestByCategory returns only the entries matching category. Stale-file diffing must
// be scoped to a single category: an upload phase for one category (e.g. images) can run before
// another phase (e.g. the engine binary) has re-recorded its own files for the current run, and
// an unscoped diff would misread the other category's not-yet-uploaded files as stale.
func filterManifestByCategory(entries []ManifestEntry, category string) []ManifestEntry {
	filtered := make([]ManifestEntry, 0, len(entries))
	for _, e := range entries {
		if e.Category == category {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// readManifest reads and parses the manifest from h. It returns nil if the manifest doesn't
// exist or can't be read.
func (p *GenericPhase) readManifest(h *cluster.ZarfHost) []ManifestEntry {
	if !h.FileExist(manifestFile) {
		return nil
	}
	content, err := h.ReadFile(manifestFile)
	if err != nil {
		return nil
	}
	return parseManifest(content)
}

// recordManifestEntry appends path to h's on-disk manifest and to h's in-memory record of what
// was uploaded during the current run (used by removeStaleManifestEntries to find files an
// upgrade no longer needs). Failures are logged and swallowed: a missed manifest entry should
// never fail an upload.
func (p *GenericPhase) recordManifestEntry(ctx context.Context, h *cluster.ZarfHost, category, path string) {
	if category == "" {
		category = "file"
	}

	h.Metadata.UploadedFiles = append(h.Metadata.UploadedFiles, category+manifestSep+path)

	entries := mergeManifest(append(p.readManifest(h), ManifestEntry{Category: category, Path: path}))

	if err := p.ensureDir(ctx, h, manifestDir, "0755", ""); err != nil {
		logger.From(ctx).Warn("failed to create upload manifest directory", "host", h, "error", err)
		return
	}
	if err := h.WriteFile(manifestFile, encodeManifest(entries), "0644"); err != nil {
		logger.From(ctx).Warn("failed to record upload manifest entry", "host", h, "path", path, "error", err)
	}
}

// removeStaleManifestEntries deletes files on h that appear in old but not in current, e.g. an
// engine binary left behind after a version bump renamed it.
func (p *GenericPhase) removeStaleManifestEntries(ctx context.Context, h *cluster.ZarfHost, old, current []ManifestEntry) {
	for _, e := range diffManifest(old, current) {
		logger.From(ctx).Info("removing stale upload from previous install", "host", h, "category", e.Category, "path", e.Path)
		if err := h.DeleteFile(e.Path); err != nil {
			logger.From(ctx).Warn("failed to remove stale upload", "host", h, "path", e.Path, "error", err)
		}
	}
}

// cleanUploadManifest deletes every file listed in h's upload manifest, then the manifest
// itself. Used during uninstall to remove files an engine's own data/config directories don't
// cover, such as binaries staged outside the distro's data dir.
func (p *GenericPhase) cleanUploadManifest(ctx context.Context, h *cluster.ZarfHost) {
	entries := p.readManifest(h)
	if len(entries) == 0 {
		return
	}

	for _, e := range entries {
		logger.From(ctx).Info("removing uploaded file", "host", h, "category", e.Category, "path", e.Path)
		if err := h.DeleteFile(e.Path); err != nil {
			logger.From(ctx).Warn("failed to remove uploaded file", "host", h, "path", e.Path, "error", err)
		}
	}

	if err := h.DeleteFile(manifestFile); err != nil {
		logger.From(ctx).Warn("failed to remove upload manifest", "host", h, "error", err)
	}
}
