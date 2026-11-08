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

package assemble

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/pkg/coci"
	"github.com/colonel-byte/cargoship/pkg/images"
)

// ociOptions is what an oci:// file source needs from the build's options, which fileGrabber
// does not otherwise receive. It is one struct rather than four arguments because every one of
// them is a property of the build and none of them is a property of the file.
type ociOptions struct {
	coci.FileOptions
	// RegistryOverrides redirect the fetch by registry prefix, the same map image pulls use.
	RegistryOverrides []images.RegistryOverride
}

// ociOptions narrows the build's options to what an oci:// file source needs.
func (o AssembleOptions) ociOptions() ociOptions {
	return ociOptions{
		FileOptions: coci.FileOptions{
			RemoteOptions: o.RemoteOptions,
			CachePath:     o.CachePath,
		},
		RegistryOverrides: o.RegistryOverrides,
	}
}

// fetchOCIFile stages the bytes an oci:// file source names, writing the file the entry asks
// for to dst.
//
// src is the reference to fetch, which is not necessarily file.Source: a registry override may
// have rewritten it. The reading itself is coci.FetchFile, which is shared with the sha256sum
// command so that an operator computing a digest and a build staging the bytes resolve an
// artifact identically.
func fetchOCIFile(ctx context.Context, src string, file v1alpha1.File, arch string, dst string, opts ociOptions) error {
	err := coci.FetchFile(ctx, src, coci.FileSelector{
		Arch:        arch,
		Layer:       file.Layer,
		ExtractPath: file.ExtractPath,
	}, dst, opts.FileOptions)

	// coci reports what the reference did not answer; the definition field that answers it is
	// named here, because only this caller knows the entry is YAML with a selector in it.
	switch {
	case errors.Is(err, coci.ErrArchRequired):
		return fmt.Errorf("%w: name one in the file entry's selector.arch", err)
	case errors.Is(err, coci.ErrLayerRequired):
		return fmt.Errorf("%w: name it in the file entry's layer field", err)
	}
	return err
}

// pinnedByDigest reports whether ref names content by digest rather than by tag, which makes
// it verify itself: a registry that served different bytes would fail the digest check.
func pinnedByDigest(ref string) bool {
	_, dgst, found := strings.Cut(ref, "@")
	return found && dgst != ""
}
