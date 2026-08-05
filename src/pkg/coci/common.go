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

// Package coci contains functions for interacting with Cargoship packages stored in OCI registries, derived from github.com/zarf-dev/src/pkg/zoci.
package coci

import (
	"context"
	"path/filepath"
	"time"

	"github.com/colonel-byte/cargoship/src/pkg/coci/layers"
	"github.com/defenseunicorns/pkg/oci"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	ociDirectory "oras.land/oras-go/v2/content/oci"
)

const (
	// CargoshipConfigMediaType is the media type for the manifest config
	CargoshipConfigMediaType = "application/vnd.cargoship.config.v1+json"
	// CargoshipLayerMediaTypeBlob is the media type for all Zarf layers due to the range of possible content
	CargoshipLayerMediaTypeBlob = "application/vnd.cargoship.layer.v1.blob"
	// DefaultConcurrency is the default concurrency used for operations
	DefaultConcurrency = 6
	//DefaultRetries is the default number of retries for operations
	DefaultRetries = 1
)

const (
	defaultDelayTime    = 500 * time.Millisecond
	defaultMaxDelayTime = 8 * time.Second
)

// PublishOptions contains options for the publish operation
type PublishOptions struct {
	// Retries is the number of times to retry a failed operation
	Retries int
	// OCIConcurrency configures the amount of layers to push in parallel
	OCIConcurrency int
	// Tag allows for overriding the destination reference
	Tag string
}

// Remote is a wrapper around the Oras remote repository with cargoship specific functions
type Remote struct {
	*oci.OrasRemote
}

// NewRemote returns an oras remote repository client and context for the given url
// with cargoship opination embedded
func NewRemote(ctx context.Context, url string, platform ocispec.Platform, mods ...oci.Modifier) (*Remote, error) {
	l := logger.From(ctx)
	modifiers := append([]oci.Modifier{
		oci.WithLogger(l),
		oci.WithUserAgent("cargoship/" + config.CLIVersion),
	}, mods...)
	remote, err := oci.NewOrasRemote(url, platform, modifiers...)
	if err != nil {
		return nil, err
	}
	return &Remote{remote}, nil
}

// GetOCICacheModifier takes in a Cargoship cachePath and uses it to return an oci.WithCache modifier
func GetOCICacheModifier(ctx context.Context, cachePath string) (oci.Modifier, error) {
	ociCache, err := ociDirectory.NewWithContext(ctx, filepath.Join(cachePath, layers.ImageCacheDirectory))
	if err != nil {
		return nil, err
	}
	return oci.WithCache(ociCache), nil
}
