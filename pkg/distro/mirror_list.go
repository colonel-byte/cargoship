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
	"io"
	"net"
	"net/http"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/pkg/coci"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/types"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry"
	orasRemote "oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/errcode"
)

// ListMirroredOptions are the optional parameters to ListMirrored. The transport half is
// deliberately the same set MirrorOptions carries, so that reading a mirror back reaches exactly
// the registries writing one can.
type ListMirroredOptions struct {
	types.RemoteOptions
	// Username and Password authenticate against the target. Empty falls back to the Docker
	// configuration file.
	Username string
	Password string
	// DialContext opens the connections the listing travels over, which is how it reaches a
	// registry only a cluster node can see. See pkg/tunnel.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	// CABundle is trusted in addition to the system roots, which is how a listing reads a
	// registry serving a certificate from a private CA. See images.ClientOptions.CABundle.
	CABundle []byte
	// Name scopes the listing to one package, which also avoids the registry catalogue
	// entirely. See ListMirrored.
	Name string
}

// ListMirrored returns the record of every cargoship package mirrored into target, newest first.
//
// With a Name it reads one repository's tags. Without one it has to ask the registry for its
// catalogue, which is a wider permission than pushing: credentials that can populate a mirror are
// not always allowed to enumerate it, and the error says so rather than reporting an empty
// registry.
//
// A listing is what was recorded, not what is still there. Nothing reconciles a record against
// the images and files it describes, so content deleted from the registry by hand leaves a record
// behind that still claims it.
func ListMirrored(ctx context.Context, target string, opts ListMirroredOptions) ([]MirrorRecord, error) {
	if target == "" {
		return nil, errors.New("target registry is required")
	}

	host, prefix, _ := strings.Cut(target, "/")
	if err := (registry.Reference{Registry: host}).ValidateRegistry(); err != nil {
		return nil, fmt.Errorf("invalid target registry %q: %w", target, err)
	}

	client, plainHTTP, err := registryClient(ctx, target, MirrorOptions{
		RemoteOptions: opts.RemoteOptions,
		Username:      opts.Username,
		Password:      opts.Password,
		DialContext:   opts.DialContext,
		CABundle:      opts.CABundle,
	})
	if err != nil {
		return nil, err
	}

	// Every record lives in one repository, so a listing is one tag query whatever it is
	// scoped to. --name narrows the tags considered rather than the repositories asked for,
	// which is why it no longer needs a wider permission than reading them.
	repos := []string{strings.TrimPrefix(prefix+"/"+RecordRepo, "/")}

	var records []MirrorRecord
	for _, repo := range repos {
		found, err := recordsInRepository(ctx, host, repo, opts.Name, plainHTTP, client)
		if err != nil {
			return nil, err
		}
		records = append(records, found...)
	}

	// Newest first, because the question an operator has is almost always which version is
	// current. Ties break on the reference so that the order is stable rather than whatever the
	// registry happened to return.
	sort.SliceStable(records, func(i, j int) bool {
		if !records[i].MirroredAt.Equal(records[j].MirroredAt) {
			return records[i].MirroredAt.After(records[j].MirroredAt)
		}
		return records[i].Reference < records[j].Reference
	})
	return records, nil
}

// recordsInRepository decodes the record behind every tag of one repository.
//
// Anything that is not a record is skipped rather than failing the listing. The reserved prefix
// makes a foreign artifact unlikely, not impossible, and the config media type is what settles
// it -- so a registry somebody else also writes into still lists.
func recordsInRepository(ctx context.Context, host, repo, name string, plainHTTP bool, client *auth.Client) ([]MirrorRecord, error) {
	log := logger.From(ctx)

	repository, err := orasRemote.NewRepository(host + "/" + repo)
	if err != nil {
		return nil, fmt.Errorf("unable to read the repository %s: %w", repo, err)
	}
	repository.PlainHTTP = plainHTTP
	repository.Client = client

	var records []MirrorRecord
	if err := repository.Tags(ctx, "", func(tags []string) error {
		for _, tag := range tags {
			// Filtered on the tag before anything is fetched. A record's tag names its
			// kind, its package and its version, so a listing scoped to one package reads
			// one manifest rather than every manifest in the repository -- and a tag that
			// is not a record at all, which includes a package published into this same
			// repository, costs nothing to skip.
			kind, isRecord := kindForTag(tag)
			if !isRecord {
				continue
			}
			if name != "" && recordTagName(tag, kind) != name {
				continue
			}
			ref := fmt.Sprintf("%s/%s:%s", host, repo, tag)
			record, ok, err := fetchMirrorRecord(ctx, repository, tag)
			if err != nil {
				return fmt.Errorf("unable to read %s: %w", ref, err)
			}
			if !ok {
				log.Debug("not a mirror record, skipping", "ref", ref)
				continue
			}
			record.Reference = ref
			records = append(records, record)
		}
		return nil
	}); err != nil {
		// A repository that is not there lists nothing rather than failing. That is the whole
		// answer for a --name nothing has been mirrored under, and the right one for a
		// repository the catalogue named and something deleted in between -- a listing is a
		// snapshot either way. Anything else, including a refused credential, is reported.
		if isNotFound(err) {
			log.Debug("no such repository, so nothing is recorded in it", "repo", repo)
			return nil, nil
		}
		return nil, err
	}
	return records, nil
}

// isNotFound reports whether err is a registry saying the thing asked for does not exist.
//
// Both spellings are needed: oras returns its own sentinel from some paths and the registry's
// response from others, and a 404 from a tag listing arrives as the latter.
func isNotFound(err error) bool {
	if errors.Is(err, errdef.ErrNotFound) {
		return true
	}
	var resp *errcode.ErrorResponse
	return errors.As(err, &resp) && resp.StatusCode == http.StatusNotFound
}

// fetchMirrorRecord decodes the record one tag points at, reporting false for an artifact that is
// not one.
func fetchMirrorRecord(ctx context.Context, repository *orasRemote.Repository, tag string) (MirrorRecord, bool, error) {
	desc, err := repository.Resolve(ctx, tag)
	if err != nil {
		return MirrorRecord{}, false, err
	}
	if desc.MediaType != ocispec.MediaTypeImageManifest {
		return MirrorRecord{}, false, nil
	}

	manifestBytes, err := fetchAll(ctx, repository, desc)
	if err != nil {
		return MirrorRecord{}, false, err
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return MirrorRecord{}, false, fmt.Errorf("unable to read the manifest: %w", err)
	}
	if manifest.Config.MediaType != coci.CargoshipMirrorConfigMediaType {
		return MirrorRecord{}, false, nil
	}

	configBytes, err := fetchAll(ctx, repository, manifest.Config)
	if err != nil {
		return MirrorRecord{}, false, err
	}
	var record MirrorRecord
	if err := json.Unmarshal(configBytes, &record); err != nil {
		return MirrorRecord{}, false, fmt.Errorf("unable to read the mirror record: %w", err)
	}
	return record, true, nil
}

// fetchAll reads one descriptor's content whole. Only manifests and record configs go through
// here, both of which are kilobytes; the package content a mirror holds is never read this way.
func fetchAll(ctx context.Context, repository *orasRemote.Repository, desc ocispec.Descriptor) (_ []byte, err error) {
	rc, err := repository.Fetch(ctx, desc)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := rc.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	return io.ReadAll(io.LimitReader(rc, desc.Size))
}
