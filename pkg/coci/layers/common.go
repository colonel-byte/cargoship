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

// Package layers contains functions for interacting with Cargoship layers stored in OCI registries, derived from github.com/zarf-dev/src/pkg/zoci.
package layers

// LayerType specifies a category of layers in a Cargoship OCI package.
type LayerType string

const (
	// ImageCacheDirectory is the directory within the Zarf cache containing an OCI store
	ImageCacheDirectory = "images"
	// MetadataLayers includes zarf.yaml, signature, and checksums.
	MetadataLayers LayerType = "metadata"
	// ImageLayers includes container image blobs.
	ImageLayers LayerType = "images"
	// OSLayers includes the OS tarball.
	OSLayers LayerType = "os"
	// FileLayers includes the File tarball.
	FileLayers LayerType = "files"
)

// GetAllLayerTypes returns the complete set of layer types in a Cargoship OCI package.
func GetAllLayerTypes() []LayerType {
	return []LayerType{
		MetadataLayers,
		ImageLayers,
		OSLayers,
		FileLayers,
	}
}
