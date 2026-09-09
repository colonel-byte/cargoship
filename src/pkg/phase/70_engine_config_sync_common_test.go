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

	files   map[string]string
	modes   map[string]fs.FileMode
	statErr error
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

func TestNeedsUpdate(t *testing.T) {
	desired := map[string]distrocfg.DesiredFile{
		registriesPath: {Content: []byte("---\nmirrors: {}\n"), Mode: "0640"},
	}

	cases := map[string]struct {
		configurer *fileConfigurer
		want       bool
	}{
		"in sync": {
			configurer: &fileConfigurer{
				files: map[string]string{registriesPath: "---\nmirrors: {}\n"},
				modes: map[string]fs.FileMode{registriesPath: 0o640},
			},
		},
		"missing": {
			configurer: &fileConfigurer{},
			want:       true,
		},
		"content differs": {
			configurer: &fileConfigurer{
				files: map[string]string{registriesPath: "---\n"},
				modes: map[string]fs.FileMode{registriesPath: 0o640},
			},
			want: true,
		},
		"someone widened the permissions": {
			configurer: &fileConfigurer{
				files: map[string]string{registriesPath: "---\nmirrors: {}\n"},
				modes: map[string]fs.FileMode{registriesPath: 0o644},
			},
			want: true,
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
			p := &EngineConfigSyncHosts{desired: desired}
			h := &cluster.ZarfHost{Configurer: tc.configurer}
			require.Equal(t, tc.want, p.needsUpdate(h))
		})
	}
}
