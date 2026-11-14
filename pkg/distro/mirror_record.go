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
	"slices"
	"strings"
	"time"

	v1alpha1 "github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
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

// RecordRepo is the one repository every record lives in, under whatever prefix the target
// carries.
//
// It is reserved rather than incidental. A package published with `cargoship publish` lands at
// <registry>/<name>:<version>, so a record written there would be the same reference as a real
// package and one would silently overwrite the other.
//
// One repository rather than one per package, which is what this used to be, because discovery
// has to work on the registries people actually use. Finding records spread across repositories
// means asking for the registry's catalogue, and `/v2/_catalog` is a wider permission than
// pushing where it exists at all -- ghcr.io answers it with 403 DENIED and Docker Hub does not
// implement it, so `cargoship ls` could not list either. Records in one repository are one
// `/v2/<repo>/tags/list` away, which every registry serves and which needs no more than pull.
const RecordRepo = "cargoship"

// Record kinds, and the tag prefix each one takes.
//
// The kind is in the tag because the two describe different things to do. A published package is
// one artifact to pull whole; a mirror is loose content a node reaches by its own references. A
// consumer looking for "where do I pull the newest rancher-rke2-multi-cilium" wants the first and
// would be misled by the second, so they cannot share a tag.
//
// The `_` prefix is what keeps them out of the way of a package someone publishes into this same
// repository: a tag may begin with `_` but a version string does not, so `cargoship:1.0.0` and
// `cargoship:_pkg.rke2.1.0.0` cannot collide however the repository is otherwise used.
const (
	// RecordKindMirror is a record written by `cargoship mirror`.
	RecordKindMirror = "mirror"
	// RecordKindPackage is a record written by `cargoship publish`.
	RecordKindPackage = "package"
)

// recordKinds is every kind this version writes and recognises.
var recordKinds = []string{RecordKindMirror, RecordKindPackage}

// recordTagName reads the package name back out of a record's tag.
//
// The parse is unambiguous because a package name may not hold a dot -- the schema's pattern is
// `^[a-z0-9][a-z0-9\-]*$` -- while a version routinely does, so the first dot after the kind
// prefix is always the boundary. The record's own JSON is still the authority on the name; this
// exists so that a listing scoped to one package can skip a tag without fetching it.
func recordTagName(tag, kind string) string {
	name, _, found := strings.Cut(strings.TrimPrefix(tag, recordTagPrefix(kind)), ".")
	if !found {
		return ""
	}
	return name
}

// recordTagPrefix is the tag prefix for a kind, derived rather than mapped.
//
// Derived because the kind is published twice -- in the tag, and in the record's own JSON -- and a
// mapping between two spellings of one concept is the pair that drifts. It did: this returned
// `_pkg.` for a kind the JSON called `package`, so a consumer reading `kind: package` out of
// `cargoship ls -o json` and building a reference from it would have constructed `_package.` and
// found nothing. Deriving the prefix makes the two incapable of disagreeing.
func recordTagPrefix(kind string) string {
	return "_" + kind + "."
}

// kindForTag reads the kind back out of a tag, and reports whether the tag is a record at all.
//
// Only the kinds this version knows are recognised. A tag written by a later cargoship with a
// kind this one has never heard of is skipped rather than guessed at, which is what keeps a
// listing from reporting a record it cannot act on.
func kindForTag(tag string) (string, bool) {
	for _, kind := range recordKinds {
		if strings.HasPrefix(tag, recordTagPrefix(kind)) {
			return kind, true
		}
	}
	return "", false
}

// isRecordKind reports whether kind is one this version writes.
func isRecordKind(kind string) bool {
	return slices.Contains(recordKinds, kind)
}

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
	// Kind says what the record describes, and so what a reader should do with it: a package
	// record names one artifact to pull whole, a mirror record names loose content a node
	// reaches by its own references. It is in the stored JSON as well as the tag, so a reader
	// holding the record does not have to parse the reference it came from.
	Kind string `json:"kind"`
	Name string `json:"name"`
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

// RecordRef is where the record of one kind, package and version lives under target.
//
// Computed, never chosen, for the same reason the references in pkg/contentstore are: the write
// and the read halves have to agree without consulting each other.
//
// The name and version are bounded by the schema -- see distro.MaxNameLength -- which is what
// makes this safe to put in a tag. A tag is at most 128 characters, and the longest prefix here
// plus the two bounds comes to 121.
func RecordRef(target, kind, name, version string) (string, error) {
	if target == "" {
		return "", errors.New("target registry is required")
	}
	if name == "" || version == "" {
		return "", fmt.Errorf("a %s record needs both a package name and a version, got %q and %q", kind, name, version)
	}
	if !isRecordKind(kind) {
		return "", fmt.Errorf("unknown record kind %q", kind)
	}
	if err := v1alpha1.ValidateReferenceLengths(name, version); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s:%s%s.%s", strings.TrimSuffix(target, "/"), RecordRepo, recordTagPrefix(kind), name, version), nil
}

// writeMirrorRecord pushes the record for a finished mirror.
//
// The layer is the package's own distro.yaml, so a reader that wants the full definition -- which
// images and files it declared, what the engine was configured with -- has it without pulling the
// package back. coci.FetchDistroYAML reads it.
func writeMirrorRecord(ctx context.Context, l *layout.DistroLayout, target string, client *auth.Client, plainHTTP bool, record MirrorRecord, retries int) error {
	ref, err := RecordRef(target, record.Kind, record.Name, record.Version)
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
