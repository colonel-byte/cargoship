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
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/colonel-byte/mare/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/k0sproject/dig"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"gopkg.in/yaml.v3"
)

var (
	versionRegex          = regexp.MustCompile(`v?[0-9]+\.[0-9]+\.[0-9]+\+[a-z0-9]+`)
	ErrVersionNotDetected = errors.New("failed to get version from the distro binary")
)

func NodeLabelsMapToList(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, fmt.Sprintf("%s=%s", k, m[k]))
	}
	return keys
}

type Common struct {
	//keep-sorted start
	Binary             string
	BinaryDir          string
	Config             string
	Data               string
	ID                 string
	Service_Controller string
	Service_Worker     string
	Token              string
	//keep-sorted end
}

var ErrPathKey = errors.New("key for set path does not exist")

func (r *Common) BinaryPath() string {
	return r.BinaryDir + "/" + r.Binary
}

func (r *Common) BinaryName() string {
	return r.Binary
}

func (r *Common) ConfigPath() string {
	return r.Config
}

func (r *Common) JoinTokenPath() string {
	return r.Token
}

func (r *Common) JoinTokenPathAgent() string {
	return filepath.Join(filepath.Dir(r.Token), "agent-token")
}

func (r *Common) DataDirPath() string {
	return r.Data
}

func (r *Common) GetWorkerService() string {
	return r.Service_Worker
}

func (r *Common) GetControllerService() string {
	return r.Service_Controller
}

func (r *Common) SetPath(key string, value string) error {
	switch key {
	case Binary:
		r.Binary = value
	case BinaryDir:
		r.BinaryDir = value
	case Config:
		r.Config = value
	case Token:
		r.Token = value
	case Data:
		r.Data = value
	default:
		return ErrPathKey
	}
	return nil
}

func (r *Common) writeYAML(ctx context.Context, host cluster.ZarfHost, config dig.Mapping, path string) error {
	buf := bytes.Buffer{}
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(config); err != nil {
		logger.From(ctx).Warn("failed to marshal yaml", "host", host)
		return err
	}

	if err := host.WriteFile(path, "---\n"+buf.String(), "0600"); err != nil {
		logger.From(ctx).Warn("failed to write file", "host", host)
		return err
	}

	return nil
}
