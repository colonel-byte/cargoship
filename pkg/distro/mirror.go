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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/avast/retry-go/v4"
	v1alpha1 "github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/contentstore"
	"github.com/colonel-byte/cargoship/pkg/haulerstore"
	"github.com/colonel-byte/cargoship/pkg/images"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	zconfig "github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	"github.com/zarf-dev/zarf/src/types"
	"oras.land/oras-go/v2/registry"
	orasRemote "oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
)

// MirrorOptions are the optional parameters to Mirror.
type MirrorOptions struct {
	types.RemoteOptions
	OCIConcurrency int
	Retries        int
	// Images and Files select what is mirrored. Both false means both, which is what a command
	// line that named neither asked for.
	Images bool
	Files  bool
	// Username and Password authenticate both halves of the mirror against the target.
	// Empty falls back to the Docker configuration file.
	Username string
	Password string
	// DialContext opens the connections both halves travel over, which is how a mirror reaches
	// a registry only a cluster node can see. See images.ClientOptions.DialContext and
	// pkg/tunnel.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	// CABundle is trusted in addition to the system roots, which is how a mirror verifies a
	// registry serving a certificate from a private CA. See images.ClientOptions.CABundle.
	CABundle []byte
}

// Mirror publishes the contents of a built package into target, a registry host optionally
// followed by a repository prefix.
//
// This is not `publish`. Publish pushes the package as one OCI artifact, to be pulled back as a
// package; Mirror pushes what the package carries as individually pullable images and files, so
// that the registry a fleet is already pointed at holds the content its nodes will ask for.
//
// The reference each item lands under is computed, never chosen, and the same two rules are
// applied by anything reading a mirror back. See images.Push and contentstore.FileRef.
func Mirror(ctx context.Context, l *layout.DistroLayout, target string, opts MirrorOptions) error {
	if l == nil {
		return errors.New("a package layout is required")
	}
	if target == "" {
		return errors.New("target registry is required")
	}
	if !opts.Images && !opts.Files {
		opts.Images, opts.Files = true, true
	}

	// One client and one plain-HTTP decision for the whole mirror, the record included. The file
	// half used to make its own, which cost a second pre-authentication round trip and left
	// nothing for the record to reuse on a run that mirrored only images.
	client, plainHTTP, err := registryClient(ctx, target, opts)
	if err != nil {
		return err
	}

	record := MirrorRecord{
		SchemaVersion:    MirrorRecordSchemaVersion,
		Name:             l.Distro.Metadata.Name,
		Version:          l.Distro.Metadata.Version,
		Engine:           l.Distro.Spec.Type,
		Digest:           l.Digest(),
		CargoshipVersion: config.CLIVersion,
		MirroredAt:       time.Now().UTC(),
	}
	for _, arch := range l.Distro.Arches() {
		record.Arches = append(record.Arches, string(arch))
	}

	if opts.Images {
		pushed, err := mirrorImages(ctx, l, target, opts)
		if err != nil {
			return err
		}
		record.Images = pushed
	}
	if opts.Files {
		pushed, err := mirrorFiles(ctx, l, target, client, plainHTTP, opts)
		if err != nil {
			return err
		}
		record.Files = pushed
	}

	// A record is tagged with the package's version, so a package that declares none -- the
	// schema allows it -- cannot have one. That is not a reason to fail a mirror whose content
	// has already landed, so it is reported and skipped.
	if record.Name == "" || record.Version == "" {
		logger.From(ctx).Warn("not recording the mirror: the package declares no name or version to record it under",
			"name", record.Name, "version", record.Version)
		return nil
	}

	// Last, and only on success. A record is a claim that the content is in the registry, so a
	// mirror that got half way through must leave none rather than one nothing can act on.
	return writeMirrorRecord(ctx, l, target, client, plainHTTP, record, opts.Retries)
}

// registryClient builds the one authenticated client a mirror uses, and settles whether the
// target speaks plain HTTP.
func registryClient(ctx context.Context, target string, opts MirrorOptions) (*auth.Client, bool, error) {
	host, _, _ := strings.Cut(target, "/")
	if err := (registry.Reference{Registry: host}).ValidateRegistry(); err != nil {
		return nil, false, fmt.Errorf("invalid target registry %q: %w", target, err)
	}

	client, err := images.NewRegistryClient(ctx, []string{host}, images.ClientOptions{
		InsecureSkipTLSVerify: opts.InsecureSkipTLSVerify,
		Credential:            images.StaticCredential(host, opts.Username, opts.Password),
		DialContext:           opts.DialContext,
		CABundle:              opts.CABundle,
	})
	if err != nil {
		return nil, false, err
	}
	if opts.PlainHTTP {
		return client, true, nil
	}
	plainHTTP, err := images.ShouldUsePlainHTTP(ctx, host, client)
	if err != nil {
		return nil, false, fmt.Errorf("unable to connect to the registry %s: %w", host, err)
	}
	return client, plainHTTP, nil
}

// mirrorImages pushes every image the package carries.
//
// AllImages rather than EnabledImages: a mirror's job is to hold the content, and the images of a
// chart that is disabled today are in the package and cost nothing extra to push. Enabling that
// chart later then needs no second pass, where the alternative fails on a node at pull time.
func mirrorImages(ctx context.Context, l *layout.DistroLayout, target string, opts MirrorOptions) (int, error) {
	refs := l.Distro.Spec.Config.AllImages()
	if len(refs) == 0 {
		logger.From(ctx).Debug("the package carries no images, nothing to mirror")
		return 0, nil
	}

	imageList := make([]transform.Image, 0, len(refs))
	for _, ref := range refs {
		img, err := transform.ParseImageRef(ref)
		if err != nil {
			return 0, fmt.Errorf("unable to parse the image reference %s: %w", ref, err)
		}
		imageList = append(imageList, img)
	}

	err := images.Push(ctx, imageList, l.GetImageDirPath(), target, images.PushOptions{
		OCIConcurrency:        opts.OCIConcurrency,
		PlainHTTP:             opts.PlainHTTP,
		InsecureSkipTLSVerify: opts.InsecureSkipTLSVerify,
		Retries:               opts.Retries,
		Username:              opts.Username,
		Password:              opts.Password,
		DialContext:           opts.DialContext,
		CABundle:              opts.CABundle,
	})
	if err != nil {
		return 0, err
	}
	return len(imageList), nil
}

// mirrorFiles pushes every file the package staged, as an OCI artifact apiece.
func mirrorFiles(ctx context.Context, l *layout.DistroLayout, target string, client *auth.Client, plainHTTP bool, opts MirrorOptions) (int, error) {
	log := logger.From(ctx)

	pushed := 0
	for _, set := range []struct {
		dir   string
		files v1alpha1.Files
	}{
		{dir: config.FilesDir, files: l.Distro.Spec.Config.Files},
		{dir: config.OSDir, files: l.Distro.Spec.Config.OS.Files},
	} {
		for i, f := range set.files {
			if f == nil {
				continue
			}
			// A file with no declared source has no original reference to mirror it under, and
			// so nothing a reader could ever match it against. Inline `data:` content is the
			// usual case, and it travels in the package rather than needing a registry.
			if f.Source == "" {
				log.Debug("not mirroring a file with no source", "target", f.Target)
				continue
			}

			staged := filepath.Join(l.DirPath(), set.dir, strconv.Itoa(i), filepath.Base(f.Target))
			if err := pushFile(ctx, staged, f.Source, target, client, plainHTTP, opts.Retries); err != nil {
				return 0, fmt.Errorf("unable to mirror file %d (%s): %w", i, f.Source, err)
			}
			pushed++
		}
	}

	log.Info("done mirroring files", "count", pushed, "target", target)
	return pushed, nil
}

// pushFile uploads one staged file as a single-layer OCI artifact.
//
// The shape is hauler's, deliberately: a config blob with one of hauler's file config media
// types, and the content as one layer titled with its filename. That is what pkg/haulerstore
// already classifies as a file, and what `hauler` and `oras` can both read, so mirroring invents
// no format of its own. The source the definition declared is recorded on the manifest under
// hauler's original-ref annotation, which is the provenance a reader needs when it is handed the
// artifact rather than computing its way to it.
func pushFile(ctx context.Context, staged, source, target string, client *auth.Client, plainHTTP bool, retries int) error {
	ref, err := contentstore.FileRef(target, source)
	if err != nil {
		return err
	}
	parsed, err := registry.ParseReference(ref)
	if err != nil {
		return fmt.Errorf("invalid destination reference %q: %w", ref, err)
	}

	// Streamed, never read whole. The files a package carries are release archives and image
	// tarballs -- hundreds of megabytes each, and the rke2 images tarball alone runs to several
	// gigabytes -- so holding one in memory to hash it would be the largest allocation in the
	// program, repeated per file.
	layerDesc, open, err := describeFile(staged)
	if err != nil {
		return err
	}

	name := filepath.Base(staged)
	layerDesc.Annotations = map[string]string{ocispec.AnnotationTitle: name}

	logger.From(ctx).Info("mirroring file", "source", source, "to", ref, "name", name)

	// An empty JSON object is what hauler's own file config amounts to for this purpose: the
	// config media type is what classifies the artifact, and everything a reader needs is in
	// the layer title and the annotation.
	repo := &orasRemote.Repository{Reference: parsed, PlainHTTP: plainHTTP, Client: client}
	return pushArtifact(ctx, repo, artifact{
		ref:             ref,
		configMediaType: haulerstore.MediaTypeFileConfigHTTP,
		config:          []byte("{}"),
		layer:           layerDesc,
		open:            open,
		annotations:     map[string]string{haulerstore.AnnotationOriginalRef: source},
	}, retries)
}

// artifact is one single-layer OCI artifact to push: a config blob whose media type classifies
// it, one layer, and the manifest annotations a reader is meant to find.
type artifact struct {
	ref             string
	configMediaType string
	config          []byte
	layer           ocispec.Descriptor
	// open reopens the layer content. It is an opener rather than a reader because a retry
	// after a partial upload has to start again from the first byte.
	open        func() (io.ReadCloser, error)
	annotations map[string]string
}

// pushArtifact uploads one artifact: the config blob, the layer, then the manifest that ties
// them together, each attempt wrapped in the same backoff.
//
// The manifest goes last on purpose. It is the only part of an artifact a reader resolves by
// reference, so until it lands there is nothing to find -- which is what makes a push that fails
// part way through leave no half-readable artifact behind.
func pushArtifact(ctx context.Context, repo *orasRemote.Repository, a artifact, retries int) error {
	if retries <= 0 {
		retries = zconfig.ZarfDefaultRetries
	}

	cfgDesc := ocispec.Descriptor{
		MediaType: a.configMediaType,
		Digest:    digest.FromBytes(a.config),
		Size:      int64(len(a.config)),
	}
	if err := withRetry(ctx, retries, "artifact config", a.ref, func() error {
		return repo.Push(ctx, cfgDesc, bytes.NewReader(a.config))
	}); err != nil {
		return fmt.Errorf("unable to push the artifact config: %w", err)
	}

	pushContent := func() error {
		content, err := a.open()
		if err != nil {
			return err
		}
		// NopCloser because oras closes the reader it is handed, and the file is ours to
		// close: without this the close below is a second close on the same descriptor,
		// which fails and masks an otherwise successful push.
		pushErr := repo.Push(ctx, a.layer, io.NopCloser(content))
		if cerr := content.Close(); cerr != nil && pushErr == nil {
			pushErr = cerr
		}
		return pushErr
	}
	if err := withRetry(ctx, retries, "artifact content", a.ref, pushContent); err != nil {
		return fmt.Errorf("unable to push the artifact content: %w", err)
	}

	manifest, err := json.Marshal(ocispec.Manifest{
		Versioned:   specs.Versioned{SchemaVersion: 2},
		MediaType:   ocispec.MediaTypeImageManifest,
		Config:      cfgDesc,
		Layers:      []ocispec.Descriptor{a.layer},
		Annotations: a.annotations,
	})
	if err != nil {
		return fmt.Errorf("unable to marshal the artifact manifest: %w", err)
	}
	manifestDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.FromBytes(manifest),
		Size:      int64(len(manifest)),
	}

	if err := withRetry(ctx, retries, "artifact manifest", a.ref, func() error {
		return repo.PushReference(ctx, manifestDesc, bytes.NewReader(manifest), a.ref)
	}); err != nil {
		return fmt.Errorf("unable to push the artifact manifest: %w", err)
	}
	return nil
}

// withRetry runs op with the same backoff the image half uses, so a mirror does not give up on
// one half of its work on a link the other half would have ridden out.
func withRetry(ctx context.Context, attempts int, what, ref string, op func() error) error {
	l := logger.From(ctx)
	return retry.Do(op,
		retry.Attempts(uint(attempts)),
		retry.Delay(zconfig.ZarfDefaultRetryDelay),
		retry.MaxDelay(zconfig.ZarfDefaultRetryMaxDelay),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.OnRetry(func(n uint, err error) {
			if attempts > 1 && n+1 < uint(attempts) {
				l.Warn("retrying push", "what", what, "ref", ref, "attempt", n+1, "maxAttempts", attempts, "error", err)
			}
		}),
	)
}

// describeFile hashes the file at path without holding it in memory, and returns its descriptor
// alongside a function that opens it again for the upload.
//
// The digest has to be known before the first byte is sent -- a registry is told what it is about
// to receive -- so the file is read twice: once to hash, once to send. Two passes over a local
// file is the cheap half of the trade against buffering a multi-gigabyte archive.
func describeFile(path string) (ocispec.Descriptor, func() (io.ReadCloser, error), error) {
	f, err := os.Open(path)
	if err != nil {
		return ocispec.Descriptor{}, nil, fmt.Errorf("unable to read the staged file: %w", err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // read-only, and the digest below is what proves it was read

	info, err := f.Stat()
	if err != nil {
		return ocispec.Descriptor{}, nil, fmt.Errorf("unable to stat the staged file: %w", err)
	}
	dgst, err := digest.FromReader(f)
	if err != nil {
		return ocispec.Descriptor{}, nil, fmt.Errorf("unable to digest the staged file: %w", err)
	}

	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageLayer,
		Digest:    dgst,
		Size:      info.Size(),
	}
	return desc, func() (io.ReadCloser, error) {
		r, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("unable to reopen the staged file: %w", err)
		}
		return r, nil
	}, nil
}
