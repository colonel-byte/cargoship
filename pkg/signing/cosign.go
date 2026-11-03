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

// Package signing provides cosign-compatible signing and verification for cargoship
// packages, against an ordinary cosign keypair.
//
// Originally a fork of zarf's src/pkg/signing, which wrapped cosign's CLI command
// structs. Those are gone: the command structs carried Fulcio, OIDC, cloud KMS,
// transparency-log and policy-evaluation machinery that cannot be reached from the
// air-gapped management node cargoship runs on, and that linked Open Policy Agent,
// cuelang, go-github and the GitLab client into the binary. Signing and verification
// now go through sigstore-go in signkey.go and verifykey.go.
//
// A cosign keypair is the only supported signing method. Keyless (Fulcio/OIDC)
// signing and verification were removed; see docs/agent/choice-signing-fork.md and
// https://github.com/colonel-byte/cargoship/issues/848.
package signing

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sigstore/cosign/v3/pkg/cosign"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// CosignDefaultTimeout is the default timeout for sign and verify operations.
const CosignDefaultTimeout = 3 * time.Minute

// nonPromptingPassFunc resolves a private-key password without ever blocking on
// terminal or stdin input. A package build is not interactive.
var nonPromptingPassFunc = cosign.PassFunc(func(_ bool) ([]byte, error) {
	if pw, ok := os.LookupEnv("COSIGN_PASSWORD"); ok {
		return []byte(pw), nil
	}
	return nil, nil
})

// SignBlobOptions holds the signing configuration for a blob.
type SignBlobOptions struct {
	// Key is the path to a cosign-format private key.
	Key string
	// BundlePath is where the Sigstore bundle is written.
	BundlePath string
	// Password decrypts the private key. PassFunc takes precedence, and
	// COSIGN_PASSWORD is the fallback when neither is set.
	Password string
	// PassFunc resolves the private-key password, taking precedence over Password.
	PassFunc cosign.PassFunc
	// Overwrite allows CheckOverwrite to replace an existing bundle.
	Overwrite bool
	// Timeout bounds the whole operation.
	Timeout time.Duration

	// Deprecated: use Key. Removed in a future version.
	KeyRef string
}

// VerifyBlobOptions holds the verification configuration for a blob.
type VerifyBlobOptions struct {
	// Key is the path to a PEM-encoded cosign public key.
	Key string
	// BundlePath is the Sigstore bundle to verify against.
	BundlePath string
	// Timeout bounds the whole operation.
	Timeout time.Duration

	// Deprecated: use Key. Removed in a future version.
	KeyRef string
}

// CheckOverwrite errors if the bundle already exists and Overwrite is false.
func (opts SignBlobOptions) CheckOverwrite(ctx context.Context) error {
	if opts.BundlePath == "" {
		return nil
	}
	if _, err := os.Stat(opts.BundlePath); err == nil {
		if !opts.Overwrite {
			return fmt.Errorf("file at path %s already exists", opts.BundlePath)
		}
		logger.From(ctx).Debug("overwriting existing file", "path", opts.BundlePath)
	}
	return nil
}

// DefaultSignBlobOptions returns the signing defaults.
func DefaultSignBlobOptions() SignBlobOptions {
	return SignBlobOptions{Timeout: CosignDefaultTimeout}
}

// DefaultVerifyBlobOptions returns the verification defaults.
func DefaultVerifyBlobOptions() VerifyBlobOptions {
	return VerifyBlobOptions{Timeout: CosignDefaultTimeout}
}

// resolvePassword resolves the private-key password, preferring an explicit PassFunc,
// then an explicit Password, then COSIGN_PASSWORD. The order is cosign's, kept so a
// caller that worked before works now.
func resolvePassword(opts SignBlobOptions) (string, error) {
	passFunc := nonPromptingPassFunc
	switch {
	case opts.PassFunc != nil:
		passFunc = opts.PassFunc
	case opts.Password != "":
		return opts.Password, nil
	}
	pw, err := passFunc(false)
	if err != nil {
		return "", fmt.Errorf("resolving the private-key password: %w", err)
	}
	return string(pw), nil
}

// syncDeprecatedKey copies KeyRef onto Key when only the deprecated alias was set.
func syncDeprecatedKey(ctx context.Context, key, keyRef, field string) string {
	if keyRef == "" {
		return key
	}
	logger.From(ctx).Warn(field + " is deprecated, use Key (removed in v1.0)")
	if key == "" {
		return keyRef
	}
	return key
}

// CosignSignBlobWithOptions signs a blob with a cosign keypair and writes a Sigstore
// bundle beside it.
func CosignSignBlobWithOptions(ctx context.Context, blobPath string, opts SignBlobOptions) ([]byte, error) {
	l := logger.From(ctx)
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	opts.Key = syncDeprecatedKey(ctx, opts.Key, opts.KeyRef, "SignBlobOptions.KeyRef")
	if opts.Key == "" {
		return nil, fmt.Errorf("a signing key is required: set SignBlobOptions.Key")
	}
	if opts.BundlePath == "" {
		return nil, fmt.Errorf("a bundle path is required: set SignBlobOptions.BundlePath")
	}

	password, err := resolvePassword(opts)
	if err != nil {
		return nil, err
	}

	l.Debug("signing blob with cosign key", "key", opts.Key, "bundlePath", opts.BundlePath)
	if err := SignBlobWithKey(ctx, opts.Key, password, blobPath, opts.BundlePath); err != nil {
		return nil, err
	}
	l.Debug("blob signed successfully")

	// The signature lives in the bundle. The byte slice is retained so callers that
	// ignore it keep compiling; nothing in this repo reads it.
	return nil, nil
}

// CosignVerifyBlobWithOptions verifies a blob against a cosign public key and the
// Sigstore bundle beside it.
func CosignVerifyBlobWithOptions(ctx context.Context, blobPath string, opts VerifyBlobOptions) error {
	l := logger.From(ctx)
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	opts.Key = syncDeprecatedKey(ctx, opts.Key, opts.KeyRef, "VerifyBlobOptions.KeyRef")
	if opts.Key == "" {
		return fmt.Errorf("a public key is required to verify: pass --key")
	}
	if opts.BundlePath == "" {
		return fmt.Errorf("a signature bundle is required to verify: set VerifyBlobOptions.BundlePath")
	}

	l.Debug("verifying blob with cosign key", "key", opts.Key, "bundlePath", opts.BundlePath)
	if err := VerifyBlobWithKey(opts.Key, opts.BundlePath, blobPath); err != nil {
		return err
	}
	l.Debug("blob signature verified successfully")
	return nil
}
