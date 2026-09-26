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
	"time"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/stretchr/testify/require"
)

const registriesPath = "/etc/rancher/rke2/registries.yaml"

// fileFS answers the file questions drift detection asks, and panics on anything else through
// the nil embedded interface.
type fileFS struct {
	remotefs.FS

	files    map[string]string
	modes    map[string]fs.FileMode
	statErr  error
	writeErr error
}

func (f *fileFS) FileExist(path string) bool {
	_, ok := f.files[path]
	return ok
}

func (f *fileFS) ReadFile(path string) ([]byte, error) {
	content, ok := f.files[path]
	if !ok {
		return nil, errors.New("no such file")
	}
	return []byte(content), nil
}

func (f *fileFS) WriteFile(path string, data []byte, _ fs.FileMode) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	if f.files == nil {
		f.files = make(map[string]string)
	}
	f.files[path] = string(data)
	return nil
}

// statFileInfo is the fs.FileInfo fileFS.Stat returns. Only Mode is read by drift detection, so
// everything else answers with a zero value.
type statFileInfo struct {
	name string
	mode fs.FileMode
}

func (i statFileInfo) Name() string       { return i.name }
func (i statFileInfo) Size() int64        { return 0 }
func (i statFileInfo) Mode() fs.FileMode  { return i.mode }
func (i statFileInfo) ModTime() time.Time { return time.Time{} }
func (i statFileInfo) IsDir() bool        { return false }
func (i statFileInfo) Sys() any           { return nil }

func (f *fileFS) Stat(path string) (fs.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	mode, ok := f.modes[path]
	if !ok {
		return nil, errors.New("no such file")
	}
	return statFileInfo{name: path, mode: mode}, nil
}

// managedDirsDistro is a distro that owns no directories, so drift detection in these tests
// comes down to the desired files themselves.
type managedDirsDistro struct {
	distrocfg.Distro

	dirs []distrocfg.ManagedDir
}

func (d *managedDirsDistro) ManagedDirs() []distrocfg.ManagedDir { return d.dirs }

func newSyncPhase(fsys *fileFS, desired map[string]distrocfg.DesiredFile) (*EngineConfigSyncHosts, *cluster.ZarfHost) {
	p := &EngineConfigSyncHosts{Distro: &managedDirsDistro{}, desired: desired}
	h := &cluster.ZarfHost{}
	h.SetFS(fsys)
	return p, h
}

func TestDriftedFiles(t *testing.T) {
	desired := map[string]distrocfg.DesiredFile{
		registriesPath: {Content: []byte("---\nmirrors: {}\n"), Mode: "0640"},
	}

	cases := map[string]struct {
		fsys *fileFS
		want []string
	}{
		"in sync": {
			fsys: &fileFS{
				files: map[string]string{registriesPath: "---\nmirrors: {}\n"},
				modes: map[string]fs.FileMode{registriesPath: 0o640},
			},
		},
		"missing": {
			fsys: &fileFS{},
			want: []string{"registries.yaml (missing)"},
		},
		"content differs": {
			fsys: &fileFS{
				files: map[string]string{registriesPath: "---\n"},
				modes: map[string]fs.FileMode{registriesPath: 0o640},
			},
			want: []string{"registries.yaml (out of sync)"},
		},
		"someone widened the permissions": {
			fsys: &fileFS{
				files: map[string]string{registriesPath: "---\nmirrors: {}\n"},
				modes: map[string]fs.FileMode{registriesPath: 0o644},
			},
			want: []string{"registries.yaml (wrong mode)"},
		},
		"a mode that cannot be read is not drift on its own": {
			fsys: &fileFS{
				files:   map[string]string{registriesPath: "---\nmirrors: {}\n"},
				statErr: errors.New("permission denied"),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p, h := newSyncPhase(tc.fsys, desired)
			require.Equal(t, tc.want, p.driftedFiles(h))
		})
	}
}

// A host is selected on its own drift, and the reason reported for it is its own. The two hosts
// here differ only in what is on them: neither carries a hostname override, which is the field
// the drift record used to be keyed by.
func TestDriftReasonIsPerHost(t *testing.T) {
	desired := map[string]distrocfg.DesiredFile{
		registriesPath: {Content: []byte("---\nmirrors: {}\n"), Mode: "0640"},
	}
	p := &EngineConfigSyncHosts{Distro: &managedDirsDistro{}, desired: desired}

	inSync := &cluster.ZarfHost{}
	inSync.SetFS(&fileFS{
		files: map[string]string{registriesPath: "---\nmirrors: {}\n"},
		modes: map[string]fs.FileMode{registriesPath: 0o640},
	})
	drifted := &cluster.ZarfHost{}
	drifted.SetFS(&fileFS{})

	require.False(t, p.needsUpdate(context.Background(), inSync))
	require.True(t, p.needsUpdate(context.Background(), drifted))

	require.Empty(t, p.driftReason(inSync), "a host with nothing to write has no drift to report")
	require.Equal(t, "registries.yaml (missing)", p.driftReason(drifted))
}

func TestNeedsUpdateNoRestartOnlyWritesDirectly(t *testing.T) {
	desired := map[string]distrocfg.DesiredFile{
		distrocfg.DistroReleaseFile: {Content: []byte(`{"name":"rke2"}`), Mode: "0600", NoRestart: true},
	}
	p := &EngineConfigSyncHosts{Distro: &managedDirsDistro{}, desired: desired}

	fsys := &fileFS{files: map[string]string{}}
	h := &cluster.ZarfHost{}
	h.SetFS(fsys)

	// Should not trigger node update/drain
	require.False(t, p.needsUpdate(context.Background(), h))
	// But should have written the NoRestart file directly
	require.JSONEq(t, `{"name":"rke2"}`, fsys.files[distrocfg.DistroReleaseFile])
}

// A NoRestart file that cannot be written in place is still drift: the host is reported as
// needing an update so the file is written on the path that drains and rewrites it.
func TestNeedsUpdateNoRestartWriteFailureKeepsHost(t *testing.T) {
	desired := map[string]distrocfg.DesiredFile{
		distrocfg.DistroReleaseFile: {Content: []byte(`{"name":"rke2"}`), Mode: "0600", NoRestart: true},
	}
	p := &EngineConfigSyncHosts{Distro: &managedDirsDistro{}, desired: desired}

	fsys := &fileFS{files: map[string]string{}, writeErr: errors.New("write failed")}
	h := &cluster.ZarfHost{}
	h.SetFS(fsys)

	require.True(t, p.needsUpdate(context.Background(), h))
	require.NotContains(t, fsys.files, distrocfg.DistroReleaseFile)
}

// A controller is checked against the files only it carries; an agent is not, so a chart
// configuration that belongs on a controller is never reported missing from an agent.
func TestFilesForByRole(t *testing.T) {
	const chartConfig = "/var/lib/rancher/rke2/server/manifests/rke2-cilium-config.yaml"

	p := &EngineConfigSyncHosts{
		Distro:  &managedDirsDistro{},
		desired: map[string]distrocfg.DesiredFile{registriesPath: {Content: []byte("---\n")}},
		controllerDesired: map[string]distrocfg.DesiredFile{
			registriesPath: {Content: []byte("---\n")},
			chartConfig:    {Content: []byte("---\n"), NoRestart: true},
		},
	}

	controller := &cluster.ZarfHost{Role: cluster.RoleController}
	require.Contains(t, p.filesFor(controller), chartConfig)

	worker := &cluster.ZarfHost{Role: cluster.RoleWorker}
	require.NotContains(t, p.filesFor(worker), chartConfig)
	require.Contains(t, p.filesFor(worker), registriesPath)
}

// TestNeedsUpdateNoRestartMarksThePhaseChanged covers the half of the changed signal that is
// easy to lose. The host is not reported as needing a sync, so without this the run reads as a
// run in which nothing happened -- and a values change landing entirely in chart manifests is
// exactly that shape.
func TestNeedsUpdateNoRestartMarksThePhaseChanged(t *testing.T) {
	desired := map[string]distrocfg.DesiredFile{
		distrocfg.DistroReleaseFile: {Content: []byte(`{"name":"rke2"}`), Mode: "0600", NoRestart: true},
	}
	p := &EngineConfigSyncHosts{Distro: &managedDirsDistro{}, desired: desired}
	h := &cluster.ZarfHost{}
	h.SetFS(&fileFS{files: map[string]string{}})

	require.False(t, p.Changed(), "nothing has been looked at yet")
	require.False(t, p.needsUpdate(context.Background(), h))
	require.True(t, p.Changed(), "the file was written in place, which is a change")
}

// TestNeedsUpdateWritesNothingUnderADryRun pins the one place a dry run could have touched a
// host. Prepare runs for every phase, including the ones a dry run is about to skip, and the
// in-place write lives in Prepare.
func TestNeedsUpdateWritesNothingUnderADryRun(t *testing.T) {
	desired := map[string]distrocfg.DesiredFile{
		distrocfg.DistroReleaseFile: {Content: []byte(`{"name":"rke2"}`), Mode: "0600", NoRestart: true},
	}
	p := &EngineConfigSyncHosts{Distro: &managedDirsDistro{}, desired: desired}
	p.manager = &Manager{DryRun: true}

	fsys := &fileFS{files: map[string]string{}}
	h := &cluster.ZarfHost{}
	h.SetFS(fsys)

	require.False(t, p.needsUpdate(context.Background(), h))
	require.NotContains(t, fsys.files, distrocfg.DistroReleaseFile, "a dry run must not write to a host")
	require.False(t, p.Changed(), "a dry run changes nothing, so it reports nothing")
}

// TestChangedIsFalseWhenNothingDrifted is the answer that makes the signal worth having: a fleet
// already in the desired state reports changed false rather than true-by-convention.
func TestChangedIsFalseWhenNothingDrifted(t *testing.T) {
	content := `{"name":"rke2"}`
	desired := map[string]distrocfg.DesiredFile{
		distrocfg.DistroReleaseFile: {Content: []byte(content), Mode: "0600", NoRestart: true},
	}
	p := &EngineConfigSyncHosts{Distro: &managedDirsDistro{}, desired: desired}
	h := &cluster.ZarfHost{}
	h.SetFS(&fileFS{
		files: map[string]string{distrocfg.DistroReleaseFile: content},
		modes: map[string]fs.FileMode{distrocfg.DistroReleaseFile: 0o600},
	})

	require.False(t, p.needsUpdate(context.Background(), h))
	require.False(t, p.Changed())
}

// TestSyncPhasesReportChange pins that the two phases an operator actually runs implement the
// interface. Losing it is silent: the phase keeps working and the run starts reporting no change.
func TestSyncPhasesReportChange(t *testing.T) {
	for _, p := range []Phase{&EngineConfigSyncController{}, &EngineConfigSyncWorker{}} {
		_, ok := p.(changedReporter)
		require.True(t, ok, "%s must report whether it changed the fleet", p.Title())
	}
}
