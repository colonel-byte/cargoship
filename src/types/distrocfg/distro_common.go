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
	"regexp"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/k0sproject/dig"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"gopkg.in/yaml.v3"
)

var (
	versionRegex = regexp.MustCompile(`v?[0-9]+\.[0-9]+\.[0-9]+\+[a-z0-9]+`)
	// ErrVersionNotDetected if a version is not detected
	ErrVersionNotDetected = errors.New("failed to get version from the distro binary")
	// ErrPathKey if a path key is not used
	ErrPathKey = errors.New("key for set path does not exist")
)

// NodeLabelsMapToList takes a map and returns a string array for used by Kubernetes labels
func NodeLabelsMapToList(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, fmt.Sprintf("%s=%s", k, m[k]))
	}
	return keys
}

// Common for all the distro's
type Common struct {
	//keep-sorted start
	// Binary name of the engine binary
	Binary string
	// BinaryDir where the engine binary is stored in
	BinaryDir string
	// Config where the engine config is located
	Config string
	// Data where the engine data is located
	Data string
	// ID the id used to identify the distro
	ID string
	// ServiceController the controller service
	ServiceController string
	// ServiceWorker the worker service
	ServiceWorker string
	// Token the token path
	Token string
	//keep-sorted end
}

// BinaryPath returns the full path to the engine binary
func (r *Common) BinaryPath() string {
	return r.BinaryDir + "/" + r.Binary
}

// BinaryName returns the engine binary name
func (r *Common) BinaryName() string {
	return r.Binary
}

// ConfigPath returns the full path for the config directory used by the engine
func (r *Common) ConfigPath() string {
	return r.Config
}

// JoinTokenPath returns the path of the token to join the cluster
func (r *Common) JoinTokenPath() string {
	return r.Token
}

// DataDirPath returns the full path for the data directory used by the engine
func (r *Common) DataDirPath() string {
	return r.Data
}

// GetWorkerService returns the name of the worker service
func (r *Common) GetWorkerService() string {
	return r.ServiceWorker
}

// GetControllerService returns the name of the controller service
func (r *Common) GetControllerService() string {
	return r.ServiceController
}

// SetPath takes in a key value pair to change how the distro values are configured, if a key is not valid it will throw an "ErrPathKey" error
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
