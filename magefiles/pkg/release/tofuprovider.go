// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package release publishes release artifacts that goreleaser does not build.
//
// Only the OpenTofu provider lives here. GoReleaser builds every other binary the repository
// releases and could build this one too, but what has to be published is not a release asset: it
// is an OCI image index assembled to the shape an OpenTofu provider mirror serves, and goreleaser
// has nothing to say about composing one. Building the zips here as well keeps the whole artifact
// in one place, and lets them carry a fixed entry timestamp so the same commit produces the same
// bytes.
package release

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/magefile/mage/sh"
)

const (
	// providerPackage is the main package the provider is built from. It is a package of this
	// module rather than a module of its own, which is why this is an ordinary package path and
	// the build below needs no -C. See docs/agent/choice-tofu-provider-layout.md.
	providerPackage = "./cmd/terraform-provider-cargoship"
	// providerBinaryBase is the binary name the zip has to carry, without the version. OpenTofu
	// looks for terraform-provider-<name>_v<version> inside the package, so this is not ours to
	// choose.
	providerBinaryBase = "terraform-provider-cargoship"
	// providerRepository is where the artifact is published. It is the repository the oci_mirror
	// example in docs/agent/choice-tofu-provider-layout.md resolves to.
	providerRepository = "ghcr.io/colonel-byte/tofu-providers/colonel-byte/cargoship"
	// providerOutputDir holds the zips and the OCI layout. It is under build/, which is ignored.
	providerOutputDir = "build/tofu-provider"

	// artifactTypeIndex and artifactTypeTarget are what OpenTofu looks for when it reads a
	// provider out of an OCI registry: the index declares the first and each per-platform
	// manifest the second. A client that finds neither does not recognise the artifact as a
	// provider at all.
	artifactTypeIndex  = "application/vnd.opentofu.provider"
	artifactTypeTarget = "application/vnd.opentofu.provider-target"
	// layerMediaType is the media type of the one layer each platform manifest carries.
	layerMediaType = "archive/zip"
)

// providerPlatforms are the platforms the provider is published for. A client whose platform is
// missing from the index fails to install rather than falling back to another one, so dropping a
// platform from this list is a user-visible decision.
var providerPlatforms = [][2]string{
	// keep-sorted start
	{"darwin", "amd64"},
	{"darwin", "arm64"},
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"windows", "amd64"},
	// keep-sorted end
}

// zipEpoch is the modification time every zip entry carries. A fixed time is what makes two builds
// of the same commit produce the same bytes, which is worth more here than the real timestamp: the
// digest is what a mirror serves and what an operator compares.
var zipEpoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// Options are the inputs to TofuProvider.
type Options struct {
	// Version is the release version without the leading v: 0.1.0, or 0.1.0+build.1.
	Version string
	// Repository is the OCI repository to publish to. Empty means providerRepository.
	Repository string
	// Push copies the assembled index to Repository. False stops after the layout is built,
	// which is how the artifact shape is checked without a registry.
	Push bool
}

// TofuProvider builds the OpenTofu provider for every published platform and assembles it into an
// OCI image index an OpenTofu provider mirror can serve, optionally pushing it.
//
// The shape is defined by the provider mirror protocol: an index whose artifactType is
// application/vnd.opentofu.provider, one manifest per platform whose artifactType is
// application/vnd.opentofu.provider-target and which carries a platform property, and a single
// archive/zip layer holding the provider package. It needs no GPG signature and no registry
// listing, which is why this is the distribution path rather than a registry submission. See
// docs/agent/choice-tofu-provider-layout.md and
// https://opentofu.org/docs/cli/oci_registries/provider-mirror/.
func TofuProvider(opts Options) error {
	if err := validateVersion(opts.Version); err != nil {
		return err
	}
	if _, err := sh.Output("oras", "version"); err != nil {
		return fmt.Errorf("oras is not usable: install ORAS 1.3.0 or newer, which is what --artifact-platform needs: %w", err)
	}

	repository := opts.Repository
	if repository == "" {
		repository = providerRepository
	}

	// The OCI tag grammar has no "+", and OpenTofu reads "_" in its place.
	tag := strings.ReplaceAll(opts.Version, "+", "_")

	if err := os.RemoveAll(providerOutputDir); err != nil {
		return err
	}
	layout := filepath.Join(providerOutputDir, "layout")
	if err := os.MkdirAll(layout, 0o755); err != nil {
		return err
	}

	// The index is assembled from the tag each platform manifest was pushed under in the local
	// layout, so this collects them as they are built.
	manifestTags := make([]string, 0, len(providerPlatforms))

	for _, platform := range providerPlatforms {
		oper, arch := platform[0], platform[1]

		archive, err := buildPlatform(opts.Version, oper, arch)
		if err != nil {
			return err
		}

		manifestTag := fmt.Sprintf("%s_%s", oper, arch)
		if err := pushPlatform(layout, manifestTag, oper, arch, archive); err != nil {
			return err
		}
		manifestTags = append(manifestTags, manifestTag)
	}

	fmt.Printf("assembling the index as %s\n", tag)
	args := []string{
		"manifest", "index", "create",
		"--artifact-type", artifactTypeIndex,
		"--oci-layout", layout + ":" + tag,
	}
	if err := sh.RunV("oras", append(args, manifestTags...)...); err != nil {
		return fmt.Errorf("assembling the provider index: %w", err)
	}

	if !opts.Push {
		fmt.Printf("not pushing: the layout is at %s\n", layout)
		return nil
	}

	target := repository + ":" + tag
	fmt.Printf("pushing %s\n", target)
	if err := sh.RunV("oras", "cp", "--from-oci-layout", layout+":"+tag, target); err != nil {
		return fmt.Errorf("pushing %s: %w", target, err)
	}
	return nil
}

// validateVersion rejects what the artifact layout cannot carry. The leading v is checked because
// the tag the release-please component produces has one and the version inside the artifact must
// not: a zip holding terraform-provider-cargoship_vv0.1.0 is one no client looks for.
func validateVersion(version string) error {
	if version == "" {
		return fmt.Errorf("a version is required: the release version without the leading v, e.g. 0.1.0")
	}
	if strings.HasPrefix(version, "v") {
		return fmt.Errorf("version must not carry a leading v: got %s", version)
	}
	return nil
}

// buildPlatform compiles the provider for one platform and zips it, returning the path of the zip.
func buildPlatform(version, oper, arch string) (string, error) {
	binary := fmt.Sprintf("%s_v%s", providerBinaryBase, version)
	if oper == "windows" {
		binary += ".exe"
	}

	dir := filepath.Join(providerOutputDir, fmt.Sprintf("%s_%s", oper, arch))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	binaryPath := filepath.Join(dir, binary)

	fmt.Printf("building %s %s for %s/%s\n", providerBinaryBase, version, oper, arch)
	env := map[string]string{
		"GOOS":        oper,
		"GOARCH":      arch,
		"CGO_ENABLED": "0",
	}
	if err := sh.RunWithV(env, "go", "build",
		"-trimpath",
		"-ldflags", "-s -w",
		"-o", binaryPath,
		providerPackage,
	); err != nil {
		return "", fmt.Errorf("building the provider for %s/%s: %w", oper, arch, err)
	}

	archive := filepath.Join(providerOutputDir, fmt.Sprintf("%s_%s_%s_%s.zip", providerBinaryBase, version, oper, arch))
	if err := zipBinary(archive, binaryPath, binary); err != nil {
		return "", err
	}
	return archive, nil
}

// zipBinary writes a zip holding exactly one entry, the binary, at the root of the archive. That is
// the layout the registry protocol defines and the one OpenTofu unpacks: a nested directory, or a
// second file, is a package it will not read.
func zipBinary(archive, binaryPath, entry string) error {
	f, err := os.Create(archive)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // the Close below is the one that reports

	w := zip.NewWriter(f)
	header := &zip.FileHeader{
		Name:     entry,
		Method:   zip.Deflate,
		Modified: zipEpoch,
	}
	header.SetMode(0o755)
	entryWriter, err := w.CreateHeader(header)
	if err != nil {
		return err
	}

	binary, err := os.Open(binaryPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(entryWriter, binary); err != nil {
		if closeErr := binary.Close(); closeErr != nil {
			return closeErr
		}
		return err
	}
	if err := binary.Close(); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return f.Close()
}

// pushPlatform pushes one platform's zip into the local OCI layout.
//
// It runs from the directory holding the zip, for two reasons: oras refuses an absolute path for a
// file argument, and the path it is given becomes the layer's org.opencontainers.image.title, so a
// relative one keeps the title the file name rather than wherever the build happened to run. The
// layout path is not a file argument, so it stays absolute and survives the change of directory.
func pushPlatform(layout, manifestTag, oper, arch, archive string) error {
	layoutAbs, err := filepath.Abs(layout)
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(filepath.Dir(archive)); err != nil {
		return err
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			panic(err)
		}
	}()

	// --artifact-platform is what puts the platform property on the descriptor the index carries.
	// Without it a client cannot tell which manifest is for its machine.
	return sh.RunV("oras", "push",
		"--artifact-type", artifactTypeTarget,
		"--artifact-platform", oper+"/"+arch,
		"--oci-layout", layoutAbs+":"+manifestTag,
		filepath.Base(archive)+":"+layerMediaType,
	)
}
