// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
//
// Modifications Copyright 2026 colonel-byte.
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

package config

import "path/filepath"

// Constants used in the default package layout.
const (
	DistroYAML = "distro.yaml"
	Bundle     = "distro.bundle.sig"
	Checksums  = "checksums.txt"
	IndexJSON  = "index.json"
	OCILayout  = "oci-layout"
)

var (
	// IndexPath is the path to the index.json file
	IndexPath = filepath.Join(ImagesDir, IndexJSON)
	// ImagesBlobsDir is the path to the directory containing the image blobs in the OCI package.
	ImagesBlobsDir = filepath.Join(ImagesDir, "blobs", "sha256")
	// OCILayoutPath is the path to the oci-layout file
	OCILayoutPath = filepath.Join(ImagesDir, OCILayout)
)
