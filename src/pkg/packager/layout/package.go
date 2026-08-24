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

package layout

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/internal/cfg"
	"github.com/colonel-byte/cargoship/src/pkg/helpers"
	"github.com/colonel-byte/cargoship/src/pkg/utils"
	goyaml "github.com/goccy/go-yaml"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/signing"
)

// Digest returns the OCI manifest digest for this package layout.
func (d *DistroLayout) Digest() string {
	return d.digest
}

// LoadFromTar unpacks the given archive (any compress/format) and loads it.
func LoadFromTar(ctx context.Context, tarPath string, opts DistroLayoutOptions) (*DistroLayout, error) {
	dirPath, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return nil, err
	}
	// Decompress the archive
	err = archive.Decompress(ctx, tarPath, dirPath, archive.DecompressOpts{})
	if err != nil {
		return nil, err
	}

	// 3) Delegate to the existing LoadFromDir
	return LoadFromDir(ctx, dirPath, opts)
}

// LoadFromDir loads and validates a package from the given directory path.
func LoadFromDir(ctx context.Context, dirPath string, opts DistroLayoutOptions) (*DistroLayout, error) {
	b, err := os.ReadFile(filepath.Join(dirPath, config.DistroYAML))
	if err != nil {
		return nil, err
	}
	dis, err := cfg.ParseMultiDoc(ctx, b)
	if err != nil {
		return nil, err
	}
	disLayout := &DistroLayout{
		dirPath: dirPath,
		Distro:  dis,
	}
	err = validateDistroIntegrity(disLayout)
	if err != nil {
		return nil, err
	}

	if err := disLayout.computeManifest(ctx); err != nil {
		return nil, fmt.Errorf("computing OCI manifest: %w", err)
	}

	if opts.VerificationStrategy != VerifyNever {
		verifyOptions := signing.DefaultVerifyBlobOptions()
		if opts.VerifyBlobOptions != nil {
			verifyOptions = *opts.VerifyBlobOptions
		}
		err = disLayout.VerifyPackageSignature(ctx, verifyOptions)
		if err != nil {
			// VerifyIfPossible tolerates only "nothing to verify against".
			// Tampered signatures and unsigned-with-material are always fatal.
			if opts.VerificationStrategy == VerifyIfPossible && errors.Is(err, ErrNoVerificationMaterial) {
				logger.From(ctx).Warn("package signature not verified; continuing", "reason", err.Error())
				return disLayout, nil
			}
			return nil, fmt.Errorf("signature verification failed: %w", err)
		}
	}

	return disLayout, nil
}

// Files returns a map of all the files in the package.
func (d *DistroLayout) Files() (map[string]string, error) {
	files := map[string]string{}
	err := filepath.Walk(d.dirPath, func(path string, info fs.FileInfo, _ error) error {
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(d.dirPath, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		files[path] = name
		return err
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func validateDistroIntegrity(disLayout *DistroLayout) error {
	_, err := os.Stat(filepath.Join(disLayout.dirPath, config.DistroYAML))
	if err != nil {
		return err
	}
	_, err = os.Stat(filepath.Join(disLayout.dirPath, config.Checksums))
	if err != nil {
		return err
	}
	err = helpers.SHAsMatch(filepath.Join(disLayout.dirPath, config.Checksums), disLayout.Distro.Metadata.AggregateChecksum)
	if err != nil {
		return err
	}

	packageFiles, err := disLayout.Files()
	if err != nil {
		return err
	}
	// distro.yaml is the root of trust and is always excluded from checksums.
	delete(packageFiles, filepath.Join(disLayout.dirPath, config.DistroYAML))
	// Hardcoded exclusions for backward compatibility with packages that predate
	// the ProvenanceFiles field. These can be removed once all supported
	// package versions include ProvenanceFiles.
	delete(packageFiles, filepath.Join(disLayout.dirPath, config.Checksums))
	delete(packageFiles, filepath.Join(disLayout.dirPath, config.Bundle))
	// Remove provenance files declared in the signed distro.yaml.
	// This enables forward compatibility — new files added by future CLI versions
	// are excluded from the strict check without requiring code changes.
	if disLayout.IsSigned() {
		for _, f := range disLayout.Distro.Build.ProvenanceFiles {
			delete(packageFiles, filepath.Join(disLayout.dirPath, f))
		}
	}

	b, err := os.ReadFile(filepath.Join(disLayout.dirPath, config.Checksums))
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		// If the line is empty (i.e. there is no checksum) simply skip it, this can result from a package with no images/components.
		if line == "" {
			continue
		}

		split := strings.Split(line, " ")
		if len(split) != 2 {
			return fmt.Errorf("invalid checksum line: %s", line)
		}
		sha := split[0]
		rel := split[1]
		if sha == "" || rel == "" {
			return fmt.Errorf("invalid checksum line: %s", line)
		}

		path := filepath.Join(disLayout.dirPath, rel)
		_, ok := packageFiles[path]
		if !ok {
			delete(packageFiles, path)
			continue
		}
		if !ok {
			return fmt.Errorf("file %s from checksum missing in layout", rel)
		}
		err = helpers.SHAsMatch(path, sha)
		if err != nil {
			return err
		}
		delete(packageFiles, path)
	}

	if len(packageFiles) > 0 {
		filePaths := slices.Collect(maps.Keys(packageFiles))
		return fmt.Errorf("package contains additional files not present in the checksum %s", strings.Join(filePaths, ", "))
	}

	return validateDistroPaths(disLayout.Distro)
}

// validateDistroPaths checks that package config fields used as filesystem
// path components do not contain path traversal sequences or separators.
func validateDistroPaths(dis distro.ZarfDistro) error {
	if !isCleanPath(dis.Metadata.Name) {
		return fmt.Errorf("package metadata name %q would result in an invalid path", dis.Metadata.Name)
	}
	if !isCleanPath(dis.Metadata.Version) {
		return fmt.Errorf("package metadata version %q would result in an invalid path", dis.Metadata.Version)
	}
	return nil
}

// isCleanPath returns true if s is safe to embed in a file path:
// it must not be ".." and must not contain path separators.
func isCleanPath(s string) bool {
	return s != ".." && !strings.ContainsAny(s, `/\`)
}

// normalizePermissions canonicalizes file and directory permissions in the
// package layout so archives produced from different sources (build vs pull)
// use consistent modes.
func (d *DistroLayout) normalizePermissions() error {
	return filepath.WalkDir(d.dirPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip symlinks; not currently used in packages, but avoid mutating targets.
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		// Directories and executable files are normalized to 0755 (rwxr-xr-x);
		// every other regular file is normalized to 0644 (rw-r--r--).
		mode := os.FileMode(helpers.ReadAllWriteUser)
		if entry.IsDir() {
			mode = helpers.ReadExecuteAllWriteUser
		} else {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode().Perm()&0111 != 0 {
				mode = helpers.ReadExecuteAllWriteUser
			}
		}
		return os.Chmod(path, mode)
	})
}

// IsSigned returns true if the package is signed.
// It first checks the package metadata (Build.Signed), then falls back to
// checking for the presence of a signature file for backward compatibility.
func (d *DistroLayout) IsSigned() bool {
	// Check metadata first (authoritative source)
	if d.Distro.Build.Signed != nil {
		return *d.Distro.Build.Signed
	}

	return false
}

// SignPackage signs the zarf package using cosign with the provided options.
// If the options do not indicate signing should be performed (no key material configured),
// this is a no-op and returns nil.
func (d *DistroLayout) SignPackage(ctx context.Context, opts signing.SignBlobOptions) (err error) {
	// This function updates in-memory state (Signed, ProvenanceFiles, VersionRequirements),
	// writes a signed zarf.yaml to a temp file, then renames the temp files into place.
	// A defer rolls back in-memory state on any error; disk state is restored best-effort
	// if a rename partially succeeds before a later rename fails.

	l := logger.From(ctx)

	// Check if signing should be performed based on the options
	// this is a no-op as there may be many different ways to sign
	// input validation should be performed in the calling function
	if !opts.ShouldSign() {
		l.Info("skipping package signing (no signing key material configured)")
		return nil
	}
	// Validate package layout state
	if d.dirPath == "" {
		return errors.New("invalid package layout: dirPath is empty")
	}
	if info, err := os.Stat(d.dirPath); err != nil {
		return fmt.Errorf("invalid package layout directory: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("invalid package layout: %s is not a directory", d.dirPath)
	}

	// Verify distro.yaml exists before signing
	distroYAMLPath := filepath.Join(d.dirPath, config.DistroYAML)
	if _, err := os.Stat(distroYAMLPath); err != nil {
		return fmt.Errorf("cannot access %s for signing: %w", config.DistroYAML, err)
	}

	// Save the original signed state in case we need to rollback
	var originalSigned *bool
	if d.Distro.Build.Signed != nil {
		val := *d.Distro.Build.Signed
		originalSigned = &val
	}

	// Create temporary directory for signing
	tmpDir, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return fmt.Errorf("failed to create temp directory for signing: %w", err)
	}
	defer func() {
		err = errors.Join(err, os.RemoveAll(tmpDir))
	}()

	tmpDistroYAMLPath := filepath.Join(tmpDir, config.DistroYAML)
	tmpBundlePath := filepath.Join(tmpDir, config.Bundle)

	// Update in-memory state
	signed := true
	d.Distro.Build.Signed = &signed

	originalProvenanceFiles := slices.Clone(d.Distro.Build.ProvenanceFiles)

	// Consolidated in-memory rollback — fires on any error exit via named return.
	defer func() {
		if err != nil {
			d.Distro.Build.Signed = originalSigned
			d.Distro.Build.ProvenanceFiles = originalProvenanceFiles
		}
	}()

	// Marshal package with signed:true
	b, err := goyaml.Marshal(d.Distro)
	if err != nil {
		return fmt.Errorf("failed to marshal package for signing: %w", err)
	}

	// Write to temporary file
	if err = os.WriteFile(tmpDistroYAMLPath, b, helpers.ReadWriteUser); err != nil {
		return fmt.Errorf("failed to write temp %s: %w", config.DistroYAML, err)
	}

	// Configure signing. cosign v3.1.1+ writes only the bundle when NewBundleFormat=true.
	signOpts := opts
	signOpts.NewBundleFormat = true

	actualBundlePath := filepath.Join(d.dirPath, config.Bundle)
	signOpts.BundlePath = actualBundlePath

	if err = signOpts.CheckOverwrite(ctx); err != nil {
		return err
	}

	signOpts.BundlePath = tmpBundlePath

	// Perform the signing operation on the temp file
	l.Debug("signing package", "source", tmpDistroYAMLPath, "bundle", tmpBundlePath)
	if _, err = signing.CosignSignBlobWithOptions(ctx, tmpDistroYAMLPath, signOpts); err != nil {
		return fmt.Errorf("failed to sign package: %w", err)
	}

	// Read original distro.yaml bytes for disk rollback if a subsequent rename fails.
	originalDistroYAMLBytes, err := os.ReadFile(distroYAMLPath)
	if err != nil {
		return fmt.Errorf("failed to read %s before rename: %w", config.DistroYAML, err)
	}

	// Atomically replace the actual files. On partial failure, restore disk state.
	if err = os.Rename(tmpDistroYAMLPath, distroYAMLPath); err != nil {
		return fmt.Errorf("failed to update %s after signing: %w", config.DistroYAML, err)
	}

	if err = os.Rename(tmpBundlePath, actualBundlePath); err != nil {
		if writeErr := os.WriteFile(distroYAMLPath, originalDistroYAMLBytes, helpers.ReadWriteUser); writeErr != nil {
			l.Warn("failed to restore original distro.yaml after bundle rename failure", "error", writeErr)
		}
		return fmt.Errorf("failed to move bundle after signing: %w", err)
	}

	if info, bundleErr := signing.ReadBundleInfo(actualBundlePath); bundleErr == nil {
		if info.Identity != "" {
			l.Info("keyless signed package", "identity", info.Identity, "issuer", info.Issuer)
		}
	} else {
		l.Debug("could not read bundle info after signing", "error", bundleErr)
	}

	if err := d.computeManifest(ctx); err != nil {
		return fmt.Errorf("recomputing OCI manifest after signing: %w", err)
	}

	l.Info("package signed successfully")
	return nil
}

// VerifyPackageSignature verifies the package signature
func (d *DistroLayout) VerifyPackageSignature(ctx context.Context, opts signing.VerifyBlobOptions) error {
	l := logger.From(ctx)
	l.Debug("verifying package signature")

	// Validate package layout state
	if d.dirPath == "" {
		return errors.New("invalid package layout: dirPath is empty")
	}
	if info, err := os.Stat(d.dirPath); err != nil {
		return fmt.Errorf("invalid package layout directory: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("invalid package layout: %s is not a directory", d.dirPath)
	}

	// Sync the deprecated KeyRef alias before computing hasKey so callers using
	// only KeyRef are not rejected for missing material. CosignVerifyBlobWithOptions
	// emits the deprecation warning when invoked.
	if opts.Key == "" && opts.KeyRef != "" { //nolint:staticcheck // intentional read of deprecated alias for migration sync
		opts.Key = opts.KeyRef //nolint:staticcheck // intentional read of deprecated alias for migration sync
	}

	hasKey := opts.Key != ""
	hasKeylessIdentity := opts.CertVerify.CertIdentity != "" || opts.CertVerify.CertIdentityRegexp != ""
	hasCert := opts.CertVerify.Cert != ""
	hasVerificationMaterial := hasKey || hasKeylessIdentity || hasCert

	// Handle the case where the package is not signed
	if !d.IsSigned() {
		if hasVerificationMaterial {
			// Providing material implies expecting a signature — always fatal.
			return errors.New("verification material was provided but the package is not signed")
		}
		return fmt.Errorf("package is not signed - verification cannot be performed: %w", ErrNoVerificationMaterial)
	}

	// Check for bundle format signature (preferred). Parse it once for both method
	// detection (fast-fail below) and the verify path.
	bundlePath := filepath.Join(d.dirPath, config.Bundle)
	bundleInfo, bundleErr := signing.ReadBundleInfo(bundlePath)
	hasBundleInfo := bundleErr == nil

	// Early validation: fail fast with a method-specific message before cosign emits a generic error.
	if hasBundleInfo {
		switch bundleInfo.Method {
		case signing.SigningMethodKeyless:
			if !hasKeylessIdentity && !hasCert {
				return fmt.Errorf("package was signed with keyless method; provide --certificate-identity + --certificate-oidc-issuer to verify: %w", ErrNoVerificationMaterial)
			}
		case signing.SigningMethodKey:
			if !hasKey && !hasCert {
				return fmt.Errorf("package was signed with a key; provide --key to verify: %w", ErrNoVerificationMaterial)
			}
		}
	}

	if !hasVerificationMaterial {
		return fmt.Errorf("package is signed but no verification material was provided (--key, --certificate-identity + --certificate-oidc-issuer): %w", ErrNoVerificationMaterial)
	}

	// Preserve a caller-provided staging directory while retaining Zarf's default.
	if opts.TempDir == "" {
		opts.TempDir = config.CommonOptions.TempDirectory
	}

	if hasBundleInfo {
		opts.BundlePath = bundlePath
		// Auto-enable UseSignedTimestamps when the bundle contains timestamps.
		// The bundle was signed with a TSA; using those timestamps is required to
		// verify the signature after the short-lived Fulcio cert expires.
		if bundleInfo.HasTSATimestamps && !opts.CommonVerifyOptions.UseSignedTimestamps {
			l.Debug("bundle contains TSA timestamps; enabling signed-timestamp verification automatically")
			opts.CommonVerifyOptions.UseSignedTimestamps = true
		}
		DistroYAMLPath := filepath.Join(d.dirPath, config.DistroYAML)
		return signing.CosignVerifyBlobWithOptions(ctx, DistroYAMLPath, opts)
	}
	if !errors.Is(bundleErr, os.ErrNotExist) {
		return fmt.Errorf("error checking bundle signature: %w", bundleErr)
	}

	opts.CommonVerifyOptions.NewBundleFormat = false
	DistroYAMLPath := filepath.Join(d.dirPath, config.DistroYAML)
	return signing.CosignVerifyBlobWithOptions(ctx, DistroYAMLPath, opts)
}
