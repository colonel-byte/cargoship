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

package images

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// DescriptorPlatform fills in the platform of a manifest descriptor, reading the image config
// when the descriptor does not carry one. It returns nil for an artifact that names no
// architecture, which is not an error: a signature or an SBOM has no platform to report.
//
// It is exported for a caller that merges several sources into one view of a reference and so
// has to know which platform each one's copy answers for.
func DescriptorPlatform(ctx context.Context, src content.Fetcher, desc ocispec.Descriptor) (*ocispec.Platform, error) {
	withPlatform, err := ensurePlatform(ctx, src, desc)
	if err != nil {
		//nolint:nilerr // an artifact naming no architecture is not a failure, it has no platform
		return nil, nil
	}
	if withPlatform.Platform == nil || withPlatform.Platform.Architecture == "" {
		return nil, nil
	}
	platform := *withPlatform.Platform
	return &platform, nil
}

// sourceCoverage is what a local image source holds for one reference, measured against what
// the build asked for.
type sourceCoverage struct {
	// held is every architecture the source can serve for the reference, sorted. It is empty
	// for an artifact that names no architecture at all, which covers whatever is asked.
	held []string
	// missing is every requested architecture the source cannot serve, sorted.
	missing []string
}

// complete reports whether the source can serve every architecture the build asked for.
func (c sourceCoverage) complete() bool { return len(c.missing) == 0 }

// describe spells what the source does hold, for a warning that has to say why it was passed
// over. An artifact naming no architecture never reaches this.
func (c sourceCoverage) describe() string {
	if len(c.held) == 0 {
		return "no architecture"
	}
	return strings.Join(c.held, ", ")
}

// coverSource measures what a source's copy of an image covers.
//
// A store standing in for the registry has to be able to serve every architecture the build
// wants, and until this existed nothing checked: a source holding one platform of a
// multi-architecture image was accepted for the reference as a whole, and the architectures it
// could not serve were dropped without a word. A zarf package makes that routine, since it
// carries a single metadata.architecture by construction.
//
// desc and body are what resolving the reference against the source already produced, so the
// common case costs no extra round trip. Only a bare manifest with no platform on its
// descriptor needs one more read, of its config, and that read is local.
func coverSource(ctx context.Context, src content.Fetcher, desc ocispec.Descriptor, body []byte, want []ocispec.Platform) (sourceCoverage, error) {
	held, platformed, err := sourceArches(ctx, src, desc, body)
	if err != nil {
		return sourceCoverage{}, err
	}
	// An artifact that declares no platform at all is not a multi-platform image -- a
	// signature, an SBOM, a file artifact stored as an image -- and one copy of it serves
	// every request. That is different from an artifact that does declare platforms and
	// happens to hold none the build can use: those are missing, not universal.
	if !platformed {
		return sourceCoverage{}, nil
	}

	coverage := sourceCoverage{held: held}
	for _, platform := range want {
		// An empty architecture is "whatever the artifact is", which anything satisfies.
		if platform.Architecture == "" {
			continue
		}
		if !slices.Contains(held, platform.Architecture) {
			coverage.missing = append(coverage.missing, platform.Architecture)
		}
	}
	slices.Sort(coverage.missing)
	coverage.missing = slices.Compact(coverage.missing)
	return coverage, nil
}

// sourceArches lists the architectures a source's copy of a reference can serve, and reports
// whether the artifact declares a platform at all.
//
// The two answers are not the same. An index whose only child is windows/amd64 declares
// platforms and can serve none of them to a linux build; an SBOM declares none and serves
// whatever is asked. Both yield no architectures, and conflating them would let the first
// stand in for the registry.
func sourceArches(ctx context.Context, src content.Fetcher, desc ocispec.Descriptor, body []byte) (_ []string, platformed bool, _ error) {
	if IsIndex(desc.MediaType) {
		var index ocispec.Index
		if err := json.Unmarshal(body, &index); err != nil {
			return nil, false, err
		}
		var arches []string
		for _, child := range index.Manifests {
			if child.Platform == nil || child.Platform.Architecture == "" {
				continue
			}
			// The pull asks for linux, so a child for anything else cannot answer it.
			if child.Platform.OS != "" && child.Platform.OS != "linux" {
				continue
			}
			arches = append(arches, child.Platform.Architecture)
		}
		slices.Sort(arches)
		return slices.Compact(arches), true, nil
	}

	// A single manifest. Its descriptor usually carries no platform, so the architecture comes
	// from the image config -- the same read the package layout does when it tags one.
	withPlatform, err := ensurePlatform(ctx, src, desc)
	if err != nil {
		// An artifact whose config names no architecture is not an image, and the caller's
		// existing behaviour -- one copy serves every request -- is right for it.
		return nil, false, nil
	}
	if withPlatform.Platform == nil || withPlatform.Platform.Architecture == "" {
		return nil, false, nil
	}
	return []string{withPlatform.Platform.Architecture}, true, nil
}
