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

// Package images is functionality related to interacting with oci images. This also stems from zarf-dev/zarf
package images

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/pkg/helpers"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// RegistryOverride describes an override for a specific registry.
type RegistryOverride struct {
	// Source describes the source registry.
	// May be of the form:
	// - docker.io/library
	// - docker.io
	Source string
	// Override replaces the source registry as a string prefix.
	Override string
}

// ApplyOverride rewrites ref through the first registry override whose source is a prefix of
// it, returning the rewritten reference, the override that matched, and whether one did.
//
// The first match wins, which relies on the overrides being sorted longest prefix first --
// ParseRegistryOverrides does that -- so that docker.io/library supersedes docker.io.
//
// This is shared rather than inlined because an oci:// file source is a registry reference
// too, and an operator who redirects a registry means it for everything cargoship pulls from
// there. Two copies of a prefix rule would drift.
func ApplyOverride(ref string, overrides []RegistryOverride) (string, RegistryOverride, bool) {
	for _, override := range overrides {
		if strings.HasPrefix(ref, override.Source) {
			return strings.Replace(ref, override.Source, override.Override, 1), override, true
		}
	}
	return ref, RegistryOverride{}, false
}

const (
	// DockerMediaTypeManifest is the Legacy Docker manifest format, replaced by OCI manifest
	DockerMediaTypeManifest = "application/vnd.docker.distribution.manifest.v2+json"
	// DockerMediaTypeManifestList is the legacy Docker manifest list, replaced by OCI index
	DockerMediaTypeManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
)

func buildScheme(plainHTTP bool) string {
	if plainHTTP {
		return "http"
	}
	return "https"
}

// Ping verifies if a user can connect to a registry
func Ping(ctx context.Context, plainHTTP bool, registryURL string, client *auth.Client) (err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	url := fmt.Sprintf("%s://%s/v2/", buildScheme(plainHTTP), registryURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusUnauthorized, http.StatusForbidden:
		return nil
	}
	return fmt.Errorf("could not connect to registry %s over %s. status code: %d", registryURL, buildScheme(plainHTTP), resp.StatusCode)
}

// ShouldUsePlainHTTP returns true if the registryURL is an http endpoint
// This is inspired by the Crane functionality to determine the schema to be used - https://github.com/google/go-containerregistry/blob/main/pkg/v1/remote/transport/ping.go
// Zarf relies heavily on this logic, as the internal registry communicates over HTTP, however we want Zarf to be flexible should the registry be over https in the future
func ShouldUsePlainHTTP(ctx context.Context, registryURL string, client *auth.Client) (bool, error) {
	// If the https connection works use https
	err := Ping(ctx, false, registryURL, client)
	if err == nil {
		return false, nil
	}
	logger.From(ctx).Debug("failing back to plainHTTP connection", "registryUrl", registryURL, "err", err)
	// If https regular request failed and plainHTTP is allowed check again over plainHTTP
	err2 := Ping(ctx, true, registryURL, client)
	if err2 != nil {
		return false, errors.Join(err, err2)
	}
	return true, nil
}

// IsManifest reports whether the media type represents an OCI manifest.
func IsManifest(mediaType string) bool {
	switch mediaType {
	case ocispec.MediaTypeImageManifest, DockerMediaTypeManifest:
		return true
	}
	return false
}

// IsIndex reports whether the media type represents an OCI image index.
func IsIndex(mediaType string) bool {
	switch mediaType {
	case ocispec.MediaTypeImageIndex, DockerMediaTypeManifestList:
		return true
	}
	return false
}

func addNameAnnotationsToDesc(desc ocispec.Descriptor, ref string) ocispec.Descriptor {
	if desc.Annotations == nil {
		desc.Annotations = make(map[string]string)
	}
	desc.Annotations[ocispec.AnnotationRefName] = ref
	desc.Annotations[ocispec.AnnotationBaseImageName] = ref
	return desc
}

func orasTransport(insecureSkipTLSVerify bool, responseHeaderTimeout time.Duration) (*retry.Transport, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("could not get default transport")
	}
	transport = transport.Clone()
	// Enable / Disable TLS verification based on the config
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: insecureSkipTLSVerify}
	// Users frequently run into servers hanging indefinitely, if the server doesn't send headers in 10 seconds then we timeout to avoid this
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	return retry.NewTransport(transport), nil
}

func getSizeOfImage(manifestDesc ocispec.Descriptor, manifest ocispec.Manifest) int64 {
	var totalSize int64
	totalSize += manifestDesc.Size
	for _, layer := range manifest.Layers {
		totalSize += layer.Size
	}
	totalSize += manifest.Config.Size
	return totalSize
}

// ClientOptions are the transport and credential settings NewRegistryClient needs.
type ClientOptions struct {
	InsecureSkipTLSVerify bool
	ResponseHeaderTimeout time.Duration
	// Credential authenticates every host the client is used with. Nil, which is the usual
	// case, reads credentials from the Docker configuration file instead.
	Credential auth.CredentialFunc
}

// NewRegistryClient builds the ORAS auth client every registry operation shares, and
// pre-authenticates it against each host it is about to be used with.
//
// The pre-authentication is synchronous and that is the point: some credential helpers open a
// browser, and doing this concurrently opens one tab per image from a host while authenticating
// in any one of them fails to propagate to the others. Pinging each host once here puts the
// credentials in the client's cache before any concurrent work starts.
//
// A ping failure is deliberately ignored. A host may be a stand-in that never answers -- the
// faked registry behind the docker fallback is one -- and the operation that follows is the
// honest place for a connection error to surface.
//
// It is exported because a mirror pushes files alongside images, and both halves have to
// authenticate to the target the same way or a credentialled registry takes one and refuses
// the other.
func NewRegistryClient(ctx context.Context, hosts []string, opts ClientOptions) (*auth.Client, error) {
	transport, err := orasTransport(opts.InsecureSkipTLSVerify, opts.ResponseHeaderTimeout)
	if err != nil {
		return nil, err
	}
	client := &auth.Client{
		Client: &http.Client{
			Transport: transport,
		},
		Cache: auth.NewCache(),
	}

	// A credential given by the caller was read from somewhere that knows which registry it
	// belongs to -- a zarf state secret, say -- so it answers for that host alone and the
	// Docker configuration is not consulted at all. Mixing the two would let a stale
	// docker login shadow the credential the operator just supplied.
	if opts.Credential != nil {
		client.Credential = opts.Credential
		for _, host := range helpers.Unique(hosts) {
			reg, err := remote.NewRegistry(host)
			if err != nil {
				return nil, fmt.Errorf("failed to create registry: %w", err)
			}
			reg.Client = client
			_ = reg.Ping(ctx) //nolint: errcheck
		}
		logger.From(ctx).Debug("using credentials supplied by the caller", "hosts", hosts)
		return client, nil
	}

	credStore, err := credentials.NewStoreFromDocker(credentials.StoreOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}
	client.Credential = credentials.Credential(credStore)

	if credStore.IsAuthConfigured() {
		for _, host := range helpers.Unique(hosts) {
			reg, err := remote.NewRegistry(host)
			if err != nil {
				return nil, fmt.Errorf("failed to create registry: %w", err)
			}
			reg.Client = client
			_ = reg.Ping(ctx) //nolint: errcheck
		}
	}

	logger.From(ctx).Debug("gathering credentials from default Docker config file",
		"credentialsConfigured", credStore.IsAuthConfigured())
	return client, nil
}
