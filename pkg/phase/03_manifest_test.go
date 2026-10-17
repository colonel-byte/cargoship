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
	"errors"
	"io/fs"
	"testing"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/stretchr/testify/require"
)

// removeFS answers one Remove with the error it is given, and records what it was asked to
// remove.
type removeFS struct {
	remotefs.FS

	err     error
	removed []string
}

func (r *removeFS) Remove(path string) error {
	r.removed = append(r.removed, path)
	return r.err
}

// TestDeleteRecordedUploadTreatsAMissingFileAsDone is the noise an uninstall used to make about
// work it had already done: it removes the engine's data and config directories first, so every
// file the manifest recorded inside them is gone by the time the manifest is walked, and each one
// was warned about by path.
func TestDeleteRecordedUploadTreatsAMissingFileAsDone(t *testing.T) {
	t.Parallel()

	const path = "/var/lib/rancher/k3s/agent/images/app_v1.tar"

	gone := &removeFS{err: remotefs.PathError("remove", path, fs.ErrNotExist)}
	h := &cluster.Host{Hostname: "worker1"}
	h.SetFS(gone)
	require.NoError(t, deleteRecordedUpload(context.Background(), h, path))
	require.Equal(t, []string{path}, gone.removed, "the removal was not attempted")

	// Every other reason a file would not go is still the caller's to report: a reset that
	// cannot remove what it uploaded has left the host holding it.
	denied := errors.New("permission denied")
	refused := &removeFS{err: remotefs.PathError("remove", path, denied)}
	h.SetFS(refused)
	require.ErrorIs(t, deleteRecordedUpload(context.Background(), h, path), denied)
}

func TestEncodeParseManifestRoundTrip(t *testing.T) {
	entries := []ManifestEntry{
		{Category: "engine", Path: "/usr/local/bin/k3s"},
		{Category: "image", Path: "/var/lib/rancher/k3s/agent/images/app_v1.tar"},
	}

	content := encodeManifest(entries)
	require.Equal(t, "engine\t/usr/local/bin/k3s\nimage\t/var/lib/rancher/k3s/agent/images/app_v1.tar\n", content)

	parsed := ParseManifest(content)
	require.Equal(t, entries, parsed)
}

func TestParseManifestSkipsMalformedLines(t *testing.T) {
	content := "engine\t/usr/local/bin/k3s\n\nnoseparatorhere\n  \nimage\t/tmp/app.tar\n"

	parsed := ParseManifest(content)

	require.Equal(t, []ManifestEntry{
		{Category: "engine", Path: "/usr/local/bin/k3s"},
		{Category: "image", Path: "/tmp/app.tar"},
	}, parsed)
}

func TestMergeManifestDedupesByPathKeepingOrderAndLatestCategory(t *testing.T) {
	entries := []ManifestEntry{
		{Category: "file", Path: "/etc/rancher/k3s/config.yaml"},
		{Category: "engine", Path: "/usr/local/bin/k3s"},
		{Category: "engine-updated", Path: "/usr/local/bin/k3s"},
	}

	merged := mergeManifest(entries)

	require.Equal(t, []ManifestEntry{
		{Category: "file", Path: "/etc/rancher/k3s/config.yaml"},
		{Category: "engine-updated", Path: "/usr/local/bin/k3s"},
	}, merged)
}

func TestDiffManifestFindsFilesAPreviousVersionLeftBehind(t *testing.T) {
	// Simulates an upgrade: v1 uploaded a binary named for its version, v2's upload no longer
	// produces that path because the new binary is named differently.
	old := []ManifestEntry{
		{Category: "engine", Path: "/usr/local/bin/k3s-v1"},
		{Category: "image", Path: "/var/lib/rancher/k3s/agent/images/app_v1.tar"},
		{Category: "file", Path: "/etc/rancher/k3s/config.yaml"},
	}
	current := []ManifestEntry{
		{Category: "engine", Path: "/usr/local/bin/k3s-v2"},
		{Category: "image", Path: "/var/lib/rancher/k3s/agent/images/app_v2.tar"},
		{Category: "file", Path: "/etc/rancher/k3s/config.yaml"},
	}

	stale := diffManifest(old, current)

	require.Equal(t, []ManifestEntry{
		{Category: "engine", Path: "/usr/local/bin/k3s-v1"},
		{Category: "image", Path: "/var/lib/rancher/k3s/agent/images/app_v1.tar"},
	}, stale)
}

func TestDiffManifestEmptyWhenNothingChanged(t *testing.T) {
	entries := []ManifestEntry{
		{Category: "engine", Path: "/usr/local/bin/k3s"},
	}

	require.Empty(t, diffManifest(entries, entries))
	require.Empty(t, diffManifest(nil, entries))
}

func TestFilterManifestByCategory(t *testing.T) {
	entries := []ManifestEntry{
		{Category: "engine", Path: "/usr/local/bin/k3s"},
		{Category: "image", Path: "/var/lib/rancher/k3s/agent/images/app_v1.tar"},
		{Category: "image", Path: "/var/lib/rancher/k3s/agent/images/other_v1.tar"},
		{Category: "file", Path: "/etc/rancher/k3s/config.yaml"},
	}

	require.Equal(t, []ManifestEntry{
		{Category: "image", Path: "/var/lib/rancher/k3s/agent/images/app_v1.tar"},
		{Category: "image", Path: "/var/lib/rancher/k3s/agent/images/other_v1.tar"},
	}, filterManifestByCategory(entries, "image"))

	require.Empty(t, filterManifestByCategory(entries, "data"))
}

func TestDiffManifestIsScopedByCategory(t *testing.T) {
	// Regression check for the bug the image-cleanup phase must avoid: the engine binary phase
	// hasn't re-recorded its file yet when the image phase's own stale-check runs, so an
	// unscoped diff would misread the engine binary as stale and delete a file still in use.
	old := []ManifestEntry{
		{Category: "engine", Path: "/usr/local/bin/k3s"},
		{Category: "image", Path: "/var/lib/rancher/k3s/agent/images/app_v1.tar"},
	}
	// Simulates mid-run state: the image phase re-recorded its file, the engine phase hasn't run yet.
	current := []ManifestEntry{
		{Category: "image", Path: "/var/lib/rancher/k3s/agent/images/app_v2.tar"},
	}

	oldImages := filterManifestByCategory(old, "image")
	currentImages := filterManifestByCategory(current, "image")

	require.Equal(t, []ManifestEntry{
		{Category: "image", Path: "/var/lib/rancher/k3s/agent/images/app_v1.tar"},
	}, diffManifest(oldImages, currentImages))
}
