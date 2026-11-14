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

package distro

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/coci"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"oras.land/oras-go/v2/registry"
	orasRemote "oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
)

// MirrorRecordSchemaVersion versions the record a mirror writes about itself. A reader handed a
// record with a major it does not know should say so rather than decode it. Bump the minor for a
// field added, the major for a field removed or given a new meaning.
const MirrorRecordSchemaVersion = "1.0"

// MirrorRecordRepoPrefix is the repository prefix every mirror record lives under.
//
// It is reserved rather than incidental. A package published with `cargoship publish` lands at
// <registry>/<name>:<version>, so a record written there would be the same reference as a real
// package and one would silently overwrite the other. The prefix also means listing the records
// for a known package is one tag query instead of a walk of the registry's catalogue.
const MirrorRecordRepoPrefix = "cargoship"

// MirrorRecord is what a mirror records about itself: which cargoship package's contents
// populated the registry, and when.
//
// It exists because nothing else in a mirror says. Images keep their original repository paths
// and files land under a reference computed from their source, so a registry holding a few
// hundred repositories answers "what images are here" and not "is k3s 1.36.1 loaded". One record
// per package and version makes it answerable, at a cost of a few kilobytes.
//
// The record is advisory, not a trust anchor. The distro.yaml it carries is a copy, and the
// signature that makes a package's own distro.yaml the root of trust lives in the package bundle
// rather than here -- so a reader learns what was mirrored, never that the content is intact.
type MirrorRecord struct {
	SchemaVersion string `json:"schemaVersion"`
	Name          string `json:"name"`
	// Version is the package's own version, not cargoship's. It is also the record's tag, which
	// is what lets two versions of one package be mirrored into the same registry and both be
	// listed afterwards.
	Version string   `json:"version"`
	Arches  []string `json:"arches,omitempty"`
	Engine  string   `json:"engine,omitempty"`
	// Digest is the OCI manifest digest computed over the package's contents at load. It is the
	// identity of what was mirrored, and it survives the package being repacked.
	Digest           string `json:"digest,omitempty"`
	CargoshipVersion string `json:"cargoshipVersion,omitempty"`
	// MirroredAt is when the mirror finished, which makes the record deliberately
	// non-reproducible: mirroring the same package twice writes a different manifest to the
	// same tag, and the later one is the one that is true.
	MirroredAt time.Time `json:"mirroredAt"`
	// Images and Files count what was pushed on the run that wrote this record. A run given
	// --images or --files mirrored one half, so a zero here is "not this run" rather than
	// "the package carries none".
	Images int `json:"images"`
	Files  int `json:"files"`

	// Reference is where this record was read from. It is set by ListMirrored and is not part
	// of the stored JSON, which cannot know the registry it would end up in.
	Reference string `json:"-"`
}

// MirrorRecordRef is where the record for one package and version lives under target.
//
// Computed, never chosen, for the same reason the references in pkg/contentstore are: the write
// and the read halves have to agree without consulting each other.
func MirrorRecordRef(target, name, version string) (string, error) {
	if target == "" {
		return "", errors.New("target registry is required")
	}
	if name == "" || version == "" {
		return "", fmt.Errorf("a mirror record needs both a package name and a version, got %q and %q", name, version)
	}
	return fmt.Sprintf("%s/%s/%s:%s", strings.TrimSuffix(target, "/"), MirrorRecordRepoPrefix, name, version), nil
}

// writeMirrorRecord pushes the record for a finished mirror.
//
// The layer is the package's own distro.yaml, so a reader that wants the full definition -- which
// images and files it declared, what the engine was configured with -- has it without pulling the
// package back. coci.FetchDistroYAML reads it.
func writeMirrorRecord(ctx context.Context, l *layout.DistroLayout, target string, client *auth.Client, plainHTTP bool, record MirrorRecord, retries int) error {
	ref, err := MirrorRecordRef(target, record.Name, record.Version)
	if err != nil {
		return err
	}
	parsed, err := registry.ParseReference(ref)
	if err != nil {
		return fmt.Errorf("invalid mirror record reference %q: %w", ref, err)
	}

	cfg, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("unable to marshal the mirror record: %w", err)
	}

	layerDesc, open, err := describeFile(filepath.Join(l.DirPath(), config.DistroYAML))
	if err != nil {
		return fmt.Errorf("unable to read %s for the mirror record: %w", config.DistroYAML, err)
	}
	layerDesc.MediaType = coci.CargoshipLayerMediaTypeBlob
	layerDesc.Annotations = map[string]string{ocispec.AnnotationTitle: config.DistroYAML}

	logger.From(ctx).Info("recording the mirror", "ref", ref, "images", record.Images, "files", record.Files)

	repo := &orasRemote.Repository{Reference: parsed, PlainHTTP: plainHTTP, Client: client}
	return pushArtifact(ctx, repo, artifact{
		ref:             ref,
		configMediaType: coci.CargoshipMirrorConfigMediaType,
		config:          cfg,
		layer:           layerDesc,
		open:            open,
		annotations: map[string]string{
			ocispec.AnnotationCreated: record.MirroredAt.Format(time.RFC3339),
			ocispec.AnnotationVersion: record.Version,
		},
	}, retries)
}
