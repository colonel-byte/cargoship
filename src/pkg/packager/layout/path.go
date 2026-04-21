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

package layout

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/colonel-byte/mare/src/config"
)

type DistroPath struct {
	ManifestFile string
	BaseDir      string
}

func ResolveDistroPath(path string) (DistroPath, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return DistroPath{}, fmt.Errorf("unable to access path %q: %w", path, err)
	}

	if fileInfo.IsDir() {
		// Backward compatible: directory -> distro.yaml
		return DistroPath{
			ManifestFile: filepath.Join(path, config.ZarfDistroYaml),
			BaseDir:      path,
		}, nil
	}

	// Direct file path
	return DistroPath{
		ManifestFile: path,
		BaseDir:      filepath.Dir(path),
	}, nil
}

type ClusterPath struct {
	ManifestFile string
	BaseDir      string
}

func ResolveClusterPath(path string) (ClusterPath, error) {
	_, err := os.Stat(path)
	if err != nil {
		return ClusterPath{}, fmt.Errorf("unable to access path %q: %w", path, err)
	}

	// Direct file path
	return ClusterPath{
		ManifestFile: path,
		BaseDir:      filepath.Dir(path),
	}, nil
}
