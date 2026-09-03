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
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/coci"
	"github.com/colonel-byte/cargoship/src/pkg/coci/layers"
	"github.com/colonel-byte/cargoship/src/pkg/helpers"
	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
	"github.com/defenseunicorns/pkg/oci"
	"github.com/gabriel-vasile/mimetype"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/signing"
	"github.com/zarf-dev/zarf/src/pkg/utils"
	"github.com/zarf-dev/zarf/src/types"
)

// PullOptions declares optional configuration for a Pull operation.
type PullOptions struct {
	// SHASum uniquely identifies a package based on its contents.
	SHASum string
	// Architecture is the package architecture.
	Architecture string
	// Deprecated: Use VerifyBlobOptions instead. PublicKeyPath validates the create-time signage of a package.
	PublicKeyPath string
	// VerifyBlobOptions configures package signature verification.
	VerifyBlobOptions *signing.VerifyBlobOptions
	// OCIConcurrency is the number of layers pulled in parallel
	OCIConcurrency int
	// CachePath is used to cache layers from OCI package pulls
	CachePath string
	types.RemoteOptions
	// VerificationStrategy for explicit definition
	layout.VerificationStrategy
}

// Pull takes a source URL and destination directory, fetches the Cargoship Distro package from the given sources, and returns the path to the fetched package.
func Pull(ctx context.Context, source, destination string, opts PullOptions) (string, error) {
	var err error
	l := logger.From(ctx)
	start := time.Now()

	// ensure architecture is set
	arch := config.GetArch(opts.Architecture)

	opts.CachePath, err = utils.ResolveCachePath(opts.CachePath)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(source)
	if err != nil {
		return "", err
	}
	l.Debug("", "url", u)
	if destination == "" {
		return "", fmt.Errorf("no output directory specified")
	}
	if u.Scheme == "" {
		return "", errors.New("scheme must be either oci:// or http(s)://")
	}
	if u.Host == "" {
		return "", errors.New("host cannot be empty")
	}

	disLayout, err := Load(ctx, source, LoadOptions{
		Shasum:               opts.SHASum,
		Architecture:         arch,
		VerifyBlobOptions:    opts.VerifyBlobOptions,
		VerificationStrategy: opts.VerificationStrategy,
		Output:               destination,
		OCIConcurrency:       opts.OCIConcurrency,
		RemoteOptions:        opts.RemoteOptions,
		CachePath:            opts.CachePath,
	})
	if err != nil {
		return "", err
	}
	if err := disLayout.Cleanup(); err != nil {
		return "", err
	}
	filename, err := disLayout.FileName()
	if err != nil {
		return "", err
	}
	filepath := filepath.Join(destination, filename)
	l.Debug("done packager.Pull", "source", source, "destination", destination, "duration", time.Since(start))
	return filepath, nil
}

type pullOCIOptions struct {
	Source            string
	Shasum            string
	Architecture      string
	LayerTypes        []layers.LayerType
	OCIConcurrency    int
	CachePath         string
	VerifyBlobOptions *signing.VerifyBlobOptions
	Connected         bool
	types.RemoteOptions
	layout.VerificationStrategy
}

func pullOCI(ctx context.Context, opts pullOCIOptions) (*layout.DistroLayout, error) {
	if opts.Shasum != "" {
		opts.Source = fmt.Sprintf("%s@sha256:%s", opts.Source, opts.Shasum)
	}
	cacheMod, err := coci.GetOCICacheModifier(ctx, opts.CachePath)
	if err != nil {
		return nil, err
	}
	platform := oci.PlatformForArch(opts.Architecture)
	remote, err := coci.NewRemote(ctx, opts.Source, platform, oci.WithPlainHTTP(opts.PlainHTTP), oci.WithInsecureSkipVerify(opts.InsecureSkipTLSVerify), cacheMod)
	if err != nil {
		return nil, err
	}
	desc, err := remote.ResolveRoot(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not find package %s with architecture %s: %w", opts.Source, platform.Architecture, err)
	}
	_, err = remote.ResolveRoot(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not find package %s with architecture %s: %w", opts.Source, platform.Architecture, err)
	}
	layersToPull, err := remote.AssembleLayers(ctx, layers.GetAllLayerTypes()...)
	if err != nil {
		return nil, err
	}
	dirPath, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return nil, err
	}
	_, err = remote.PullDistro(ctx, dirPath, opts.OCIConcurrency, layersToPull...)
	if err != nil {
		return nil, err
	}

	layoutOpts := layout.DistroLayoutOptions{
		VerifyBlobOptions:    opts.VerifyBlobOptions,
		VerificationStrategy: opts.VerificationStrategy,
	}
	disLayout, err := layout.LoadFromDir(ctx, dirPath, layoutOpts)
	if err != nil {
		return nil, err
	}
	// Use the digest resolved from the registry rather than recomputing from local
	// files. This is cheaper and accurate even for partial pulls where file-based
	// computation would produce a different (partial) digest.
	disLayout.SetRegistryDigest(desc.Digest.String())
	return disLayout, nil
}

func pullHTTP(ctx context.Context, src, tarDir, shasum string, insecureTLSSkipVerify bool) (string, error) {
	if shasum == "" {
		return "", errors.New("shasum cannot be empty")
	}
	tarPath := filepath.Join(tarDir, "data")

	err := pullHTTPFile(ctx, src, tarPath, insecureTLSSkipVerify)
	if err != nil {
		return "", err
	}

	received, err := helpers.GetSHA256OfFile(tarPath)
	if err != nil {
		return "", err
	}
	if received != shasum {
		return "", fmt.Errorf("shasum mismatch for file %s, expected %s but got %s", tarPath, shasum, received)
	}

	mtype, err := mimetype.DetectFile(tarPath)
	if err != nil {
		return "", err
	}

	newPath := filepath.Join(tarDir, "data.tar")

	if mtype.Is("application/x-tar") {
		err = os.Rename(tarPath, newPath)
		if err != nil {
			return "", err
		}
		return newPath, nil
	} else if mtype.Is("application/zstd") {
		newPath = fmt.Sprintf("%s.zst", newPath)
		err = os.Rename(tarPath, newPath)
		if err != nil {
			return "", err
		}
		return newPath, nil
	}
	return "", fmt.Errorf("unsupported file type: %s", mtype.Extension())
}

func pullHTTPFile(ctx context.Context, src, tarPath string, insecureTLSSkipVerify bool) (err error) {
	f, err := os.Create(tarPath)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return err
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return errors.New("could not get default transport")
	}
	transport = transport.Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: insecureTLSSkipVerify}
	client := &http.Client{Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()
	if resp.StatusCode != http.StatusOK {
		_, err := io.Copy(io.Discard, resp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected http response status code %s for source %s", resp.Status, src)
	}
	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return err
	}
	return nil
}
