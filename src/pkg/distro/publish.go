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

package distro

import (
	"context"
	"fmt"

	"github.com/colonel-byte/cargoship/src/pkg/coci"
	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
	"github.com/defenseunicorns/pkg/oci"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/signing"
	"github.com/zarf-dev/zarf/src/types"
	"oras.land/oras-go/v2/registry"
)

const defaultPublishRetries = 1

// PublishOptions are the optional parameters to publish
type PublishOptions struct {
	// OCIConcurrency configures the amount of layers to push in parallel
	OCIConcurrency int
	// Retries specifies the number of retries to use
	Retries int
	// SignBlobOptions holds all signing configuration. Use signing.DefaultSignBlobOptions() as a base.
	SignBlobOptions signing.SignBlobOptions
	CachePath       string
	IsInteractive   bool
	Registry        *registry.Reference
	types.RemoteOptions
	// Tag is an optional tag for the OCI reference separate from the package metadata.version
	Tag string
}

// Publish used to publish a distro package to an oci registry
func Publish(ctx context.Context, disLayout *layout.DistroLayout, dst registry.Reference, opts PublishOptions) (registry.Reference, error) {
	if opts.Registry == nil {
		return registry.Reference{}, fmt.Errorf("registry must not be null")
	}

	l := logger.From(ctx)

	// disallow infinite or negative
	if opts.Retries <= 0 {
		if opts.Retries < 0 {
			return registry.Reference{}, fmt.Errorf("retries cannot be negative")
		}
		l.Debug("retries set to default", "retries", defaultPublishRetries)
		opts.Retries = defaultPublishRetries
	}
	if err := dst.ValidateRegistry(); err != nil {
		return registry.Reference{}, fmt.Errorf("invalid registry: %w", err)
	}
	if disLayout == nil {
		return registry.Reference{}, fmt.Errorf("package layout must be specified")
	}

	if err := disLayout.SignPackage(ctx, opts.SignBlobOptions); err != nil {
		return registry.Reference{}, fmt.Errorf("unable to sign package: %w", err)
	}

	referenceOptions := coci.ReferenceFromMetadataOptions{
		Tag: opts.Tag,
	}
	// Build Reference for remote from registry location and pkg
	disRef, err := coci.ReferenceFromMetadataWithOptions(dst.String(), disLayout.Distro, referenceOptions)
	if err != nil {
		return registry.Reference{}, err
	}

	if err := pushToRemote(ctx, disLayout, disRef, opts); err != nil {
		return registry.Reference{}, err
	}

	return registry.Reference{}, nil
}

// pushToRemote pushes a package to the given reference
func pushToRemote(ctx context.Context, layout *layout.DistroLayout, ref registry.Reference, opts PublishOptions) error {
	arch := layout.Distro.Metadata.Architecture
	// Set platform
	platform := oci.PlatformForArch(arch)

	cacheMod, err := coci.GetOCICacheModifier(ctx, opts.CachePath)
	if err != nil {
		return fmt.Errorf("could not configure OCI cache: %w", err)
	}

	remote, err := coci.NewRemote(ctx, ref.String(), platform, oci.WithPlainHTTP(opts.PlainHTTP), oci.WithInsecureSkipVerify(opts.InsecureSkipTLSVerify), cacheMod)
	if err != nil {
		return fmt.Errorf("could not instantiate remote: %w", err)
	}

	publishOptions := coci.PublishOptions{
		OCIConcurrency: opts.OCIConcurrency,
		Retries:        opts.Retries,
	}

	_, err = remote.PushPackage(ctx, layout, publishOptions)
	if err != nil {
		return fmt.Errorf("could not push package: %w", err)
	}

	return nil
}
