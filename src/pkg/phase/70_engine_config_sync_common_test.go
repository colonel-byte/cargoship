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

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	hostos "github.com/colonel-byte/cargoship/src/types/os"
	"github.com/k0sproject/rig/exec"
	rigos "github.com/k0sproject/rig/os"
	"github.com/stretchr/testify/require"
)

const registriesPath = "/etc/rancher/rke2/registries.yaml"

// fileConfigurer answers the file questions drift detection asks, and panics on anything else
// through the nil embedded interface.
type fileConfigurer struct {
	hostos.Configurer

	files    map[string]string
	modes    map[string]fs.FileMode
	statErr  error
	writeErr error
}

func (c *fileConfigurer) FileExist(_ rigos.Host, path string) bool {
	_, ok := c.files[path]
	return ok
}

func (c *fileConfigurer) ReadFile(_ rigos.Host, path string) (string, error) {
	content, ok := c.files[path]
	if !ok {
		return "", errors.New("no such file")
	}
	return content, nil
}

func (c *fileConfigurer) WriteFile(_ rigos.Host, path string, content string, _ string) error {
	if c.writeErr != nil {
		return c.writeErr
	}
	if c.files == nil {
		c.files = make(map[string]string)
	}
	c.files[path] = content
	return nil
}

func (c *fileConfigurer) Stat(_ rigos.Host, path string, _ ...exec.Option) (*rigos.FileInfo, error) {
	if c.statErr != nil {
		return nil, c.statErr
	}
	mode, ok := c.modes[path]
	if !ok {
		return nil, errors.New("no such file")
	}
	return &rigos.FileInfo{FName: path, FMode: mode}, nil
}

// managedDirsDistro is a distro that owns no directories, so drift detection in these tests
// comes down to the desired files themselves.
type managedDirsDistro struct {
	distrocfg.Distro

	dirs []distrocfg.ManagedDir
}

func (d *managedDirsDistro) ManagedDirs() []distrocfg.ManagedDir { return d.dirs }

func newSyncPhase(cfg *fileConfigurer, desired map[string]distrocfg.DesiredFile) (*EngineConfigSyncHosts, *cluster.ZarfHost) {
	p := &EngineConfigSyncHosts{Distro: &managedDirsDistro{}, desired: desired}
	return p, &cluster.ZarfHost{Configurer: cfg}
}

func TestDriftedFiles(t *testing.T) {
	desired := map[string]distrocfg.DesiredFile{
		registriesPath: {Content: []byte("---\nmirrors: {}\n"), Mode: "0640"},
	}

	cases := map[string]struct {
		configurer *fileConfigurer
		want       []string
	}{
		"in sync": {
			configurer: &fileConfigurer{
				files: map[string]string{registriesPath: "---\nmirrors: {}\n"},
				modes: map[string]fs.FileMode{registriesPath: 0o640},
			},
		},
		"missing": {
			configurer: &fileConfigurer{},
			want:       []string{"registries.yaml (missing)"},
		},
		"content differs": {
			configurer: &fileConfigurer{
				files: map[string]string{registriesPath: "---\n"},
				modes: map[string]fs.FileMode{registriesPath: 0o640},
			},
			want: []string{"registries.yaml (out of sync)"},
		},
		"someone widened the permissions": {
			configurer: &fileConfigurer{
				files: map[string]string{registriesPath: "---\nmirrors: {}\n"},
				modes: map[string]fs.FileMode{registriesPath: 0o644},
			},
			want: []string{"registries.yaml (wrong mode)"},
		},
		"a mode that cannot be read is not drift on its own": {
			configurer: &fileConfigurer{
				files:   map[string]string{registriesPath: "---\nmirrors: {}\n"},
				statErr: errors.New("permission denied"),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p, h := newSyncPhase(tc.configurer, desired)
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

	inSync := &cluster.ZarfHost{Configurer: &fileConfigurer{
		files: map[string]string{registriesPath: "---\nmirrors: {}\n"},
		modes: map[string]fs.FileMode{registriesPath: 0o640},
	}}
	drifted := &cluster.ZarfHost{Configurer: &fileConfigurer{}}

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

	fc := &fileConfigurer{files: map[string]string{}}
	h := &cluster.ZarfHost{Configurer: fc}

	// Should not trigger node update/drain
	require.False(t, p.needsUpdate(context.Background(), h))
	// But should have written the NoRestart file directly
	require.JSONEq(t, `{"name":"rke2"}`, fc.files[distrocfg.DistroReleaseFile])
}

// A NoRestart file that cannot be written in place is still drift: the host is reported as
// needing an update so the file is written on the path that drains and rewrites it.
func TestNeedsUpdateNoRestartWriteFailureKeepsHost(t *testing.T) {
	desired := map[string]distrocfg.DesiredFile{
		distrocfg.DistroReleaseFile: {Content: []byte(`{"name":"rke2"}`), Mode: "0600", NoRestart: true},
	}
	p := &EngineConfigSyncHosts{Distro: &managedDirsDistro{}, desired: desired}

	fc := &fileConfigurer{files: map[string]string{}, writeErr: errors.New("write failed")}
	h := &cluster.ZarfHost{Configurer: fc}

	require.True(t, p.needsUpdate(context.Background(), h))
	require.NotContains(t, fc.files, distrocfg.DistroReleaseFile)
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
