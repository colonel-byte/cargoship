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

// Package signing provides cosign-based signing and verification for cargoship packages.
//
// Derived from zarf's src/pkg/signing, with two deliberate differences. The cloud KMS plugins
// (aws, azure, gcp, hashivault) and cosign's ambient OIDC credential providers are not registered
// here: all four need to reach a cloud endpoint, so none of them can be used from the air-gapped
// management node cargoship runs on, and together they account for roughly 9MB of binary that
// nothing on an air-gapped host can call. See docs/agent/choice-signing-fork.md, and
// docs/dev/signing.md for the supported signing path.
//
// SignManifest and VerifyManifest are also dropped, because cargoship signs and verifies the
// distro.yaml blob rather than an OCI manifest referrer.
package signing

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sigstore/cosign/v3/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/v3/cmd/cosign/cli/sign"
	"github.com/sigstore/cosign/v3/cmd/cosign/cli/signcommon"
	"github.com/sigstore/cosign/v3/cmd/cosign/cli/verify"
	"github.com/sigstore/cosign/v3/pkg/cosign"

	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// CosignDefaultTimeout is the default timeout for cosign sign and verify operations.
const CosignDefaultTimeout = 3 * time.Minute

// nonPromptingPassFunc resolves a private-key password without ever blocking on
// terminal or stdin input.
// nonPromptingPassFunc resolves a private-key password without ever blocking on
// terminal or stdin input.
var nonPromptingPassFunc = cosign.PassFunc(func(_ bool) ([]byte, error) {
	if pw, ok := os.LookupEnv("COSIGN_PASSWORD"); ok {
		return []byte(pw), nil
	}
	return nil, nil
})

// SignBlobOptions holds signing configuration for zarf blob operations.
// SignBlobOptions holds signing configuration for zarf blob operations.
type SignBlobOptions struct {
	Key              string
	Base64Output     bool
	OutputSignature  string
	BundlePath       string
	NewBundleFormat  bool
	SkipConfirmation bool
	TlogUpload       bool
	TSAServerURL     string
	// UseSigningConfig is set to false by DefaultSignBlobOptions to override
	// cosign's default of true, which conflicts with TlogUpload=false in airgap.
	UseSigningConfig bool

	SecurityKey options.SecurityKeyOptions
	Fulcio      options.FulcioOptions
	Rekor       options.RekorOptions
	OIDC        options.OIDCOptions

	Verbose   bool
	Timeout   time.Duration
	Password  string
	PassFunc  cosign.PassFunc
	Overwrite bool

	// Deprecated: use Key. Removed in a future version.
	KeyRef string
}

// VerifyBlobOptions holds verification configuration for zarf blob operations.
// VerifyBlobOptions holds verification configuration for zarf blob operations.
type VerifyBlobOptions struct {
	Key        string
	Signature  string
	BundlePath string

	SecurityKey         options.SecurityKeyOptions
	CertVerify          options.CertVerifyOptions
	Rekor               options.RekorOptions
	CommonVerifyOptions options.CommonVerifyOptions
	SignatureDigest     options.SignatureDigestOptions

	TempDir string
	Timeout time.Duration

	// Deprecated: use Key. Removed in a future version.
	KeyRef string
	// Deprecated: use Signature. Removed in a future version.
	SigRef string
}

// CheckOverwrite errors if any output file exists and Overwrite is false.
func (opts SignBlobOptions) CheckOverwrite(ctx context.Context) error {
	for _, path := range []string{opts.BundlePath, opts.OutputSignature} {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			if !opts.Overwrite {
				return fmt.Errorf("file at path %s already exists", path)
			}
			logger.From(ctx).Debug("overwriting existing file", "path", path)
		}
	}
	return nil
}

// DefaultSignBlobOptions returns SignBlobOptions seeded with zarf defaults.
// Divergences from cosign defaults (air-gap):
//   - TlogUpload=false (cosign default true)
//   - UseSigningConfig=false (cosign default true) — required because cosign rejects
//     UseSigningConfig=true combined with TlogUpload=false.
//
// DefaultSignBlobOptions returns SignBlobOptions seeded with zarf defaults.
// Divergences from cosign defaults (air-gap):
//   - TlogUpload=false (cosign default true)
//   - UseSigningConfig=false (cosign default true) — required because cosign rejects
//     UseSigningConfig=true combined with TlogUpload=false.
func DefaultSignBlobOptions() SignBlobOptions {
	var opts SignBlobOptions
	opts.TlogUpload = false
	opts.UseSigningConfig = false
	opts.Base64Output = true
	opts.NewBundleFormat = true
	opts.SecurityKey.Slot = "signature"
	opts.OIDC.ClientID = "sigstore"
	opts.Timeout = CosignDefaultTimeout
	return opts
}

// DefaultVerifyBlobOptions returns VerifyBlobOptions seeded with zarf defaults.
// Divergences: IgnoreTlog and IgnoreSCT default to true (cosign default false) for airgap.
// DefaultVerifyBlobOptions returns VerifyBlobOptions seeded with zarf defaults.
// Divergences: IgnoreTlog and IgnoreSCT default to true (cosign default false) for airgap.
func DefaultVerifyBlobOptions() VerifyBlobOptions {
	var opts VerifyBlobOptions
	opts.CommonVerifyOptions.IgnoreTlog = true
	opts.CertVerify.IgnoreSCT = true
	opts.CommonVerifyOptions.NewBundleFormat = true
	opts.Timeout = CosignDefaultTimeout
	return opts
}

// CosignSignBlobWithOptions signs a blob via cosign's SignBlobCmd.
// Mirrors cmd/cosign/cli/signblob.go (v3.0.6) SignBlob().RunE.
func CosignSignBlobWithOptions(ctx context.Context, blobPath string, opts SignBlobOptions) ([]byte, error) {
	l := logger.From(ctx)
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	if opts.KeyRef != "" {
		l.Warn("SignBlobOptions.KeyRef is deprecated, use Key (removed in v1.0)")
		if opts.Key == "" {
			opts.Key = opts.KeyRef
		}
	}

	rootOpts := &options.RootOptions{
		Verbose: opts.Verbose,
		Timeout: opts.Timeout,
	}

	oidcClientSecret, err := opts.OIDC.ClientSecret()
	if err != nil {
		return nil, err
	}

	ko := options.KeyOpts{
		KeyRef:           opts.Key,
		PassFunc:         nonPromptingPassFunc,
		Sk:               opts.SecurityKey.Use,
		Slot:             opts.SecurityKey.Slot,
		FulcioURL:        opts.Fulcio.URL,
		IDToken:          opts.Fulcio.IdentityToken,
		FulcioAuthFlow:   opts.Fulcio.AuthFlow,
		RekorURL:         opts.Rekor.URL,
		OIDCIssuer:       opts.OIDC.Issuer,
		OIDCClientID:     opts.OIDC.ClientID,
		OIDCClientSecret: oidcClientSecret,
		BundlePath:       opts.BundlePath,
		NewBundleFormat:  opts.NewBundleFormat,
		SkipConfirmation: opts.SkipConfirmation,
		TSAServerURL:     opts.TSAServerURL,
	}

	switch {
	case opts.PassFunc != nil:
		ko.PassFunc = opts.PassFunc
	case opts.Password != "":
		password := opts.Password
		ko.PassFunc = cosign.PassFunc(func(_ bool) ([]byte, error) {
			return []byte(password), nil
		})
	}

	if err := opts.CheckOverwrite(ctx); err != nil {
		return nil, err
	}

	// Empty output-path params suppress cosign's deprecation warnings; zarf manages
	// OutputSignature internally and the warnings would fire on every sign.
	if err := signcommon.LoadTrustedMaterialAndSigningConfig(ctx, &ko,
		opts.UseSigningConfig, "",
		opts.Rekor.URL, opts.Fulcio.URL, opts.OIDC.Issuer, opts.TSAServerURL, "",
		opts.TlogUpload, opts.NewBundleFormat, opts.BundlePath, opts.Key, false,
		"", "", "", "", "", "",
	); err != nil {
		return nil, err
	}

	l.Debug("signing blob with cosign",
		"key", opts.Key,
		"sk", opts.SecurityKey.Use,
		"bundlePath", opts.BundlePath)

	sig, err := sign.SignBlobCmd(
		ctx,
		rootOpts,
		ko,
		blobPath,
		"",
		"",
		opts.Base64Output,
		opts.OutputSignature,
		"",
		opts.TlogUpload,
	)
	if err != nil {
		return nil, err
	}

	l.Debug("blob signed successfully", "signatureLength", len(sig))
	return sig, nil
}

// CosignVerifyBlobWithOptions verifies a blob via cosign's VerifyBlobCmd.
// Mirrors cmd/cosign/cli/verify.go (v3.0.6) VerifyBlob().RunE.
func CosignVerifyBlobWithOptions(ctx context.Context, blobPath string, opts VerifyBlobOptions) error {
	l := logger.From(ctx)
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	if opts.KeyRef != "" {
		l.Warn("VerifyBlobOptions.KeyRef is deprecated, use Key (removed in v1.0)")
		if opts.Key == "" {
			opts.Key = opts.KeyRef
		}
	}
	if opts.SigRef != "" {
		l.Warn("VerifyBlobOptions.SigRef is deprecated, use Signature (removed in v1.0)")
		if opts.Signature == "" {
			opts.Signature = opts.SigRef
		}
	}

	hashAlgorithm, err := opts.SignatureDigest.HashAlgorithm()
	if err != nil {
		return err
	}

	// Keyless verify needs a trusted root. If the user didn't supply --trusted-root,
	// fall back to the embedded copy. Key-based and cert-based paths skip this
	// entirely so they don't pay for an unused tempfile per verify.
	trustedRootPath := opts.CommonVerifyOptions.TrustedRootPath
	if trustedRootPath == "" && opts.Key == "" && opts.CertVerify.Cert == "" {
		path, cleanup, prepErr := writeEmbeddedTrustedRoot(opts.TempDir)
		if prepErr != nil {
			return fmt.Errorf("preparing embedded trusted root: %w", prepErr)
		}
		defer func() {
			if rmErr := cleanup(); rmErr != nil {
				l.Debug("failed to remove embedded trusted root tempfile", "error", rmErr)
			}
		}()
		trustedRootPath = path
	}

	ko := options.KeyOpts{
		KeyRef:           opts.Key,
		Sk:               opts.SecurityKey.Use,
		Slot:             opts.SecurityKey.Slot,
		RekorURL:         opts.Rekor.URL,
		BundlePath:       opts.BundlePath,
		TSACertChainPath: opts.CommonVerifyOptions.TSACertChainPath,
		NewBundleFormat:  opts.CommonVerifyOptions.NewBundleFormat,
	}

	cmd := &verify.VerifyBlobCmd{
		KeyOpts:                      ko,
		CertVerifyOptions:            opts.CertVerify,
		CertRef:                      opts.CertVerify.Cert,
		CertChain:                    opts.CertVerify.CertChain,
		CARoots:                      opts.CertVerify.CARoots,
		CAIntermediates:              opts.CertVerify.CAIntermediates,
		SigRef:                       opts.Signature,
		CertGithubWorkflowTrigger:    opts.CertVerify.CertGithubWorkflowTrigger,
		CertGithubWorkflowSHA:        opts.CertVerify.CertGithubWorkflowSha,
		CertGithubWorkflowName:       opts.CertVerify.CertGithubWorkflowName,
		CertGithubWorkflowRepository: opts.CertVerify.CertGithubWorkflowRepository,
		CertGithubWorkflowRef:        opts.CertVerify.CertGithubWorkflowRef,
		IgnoreSCT:                    opts.CertVerify.IgnoreSCT,
		SCTRef:                       opts.CertVerify.SCT,
		Offline:                      opts.CommonVerifyOptions.Offline,
		IgnoreTlog:                   opts.CommonVerifyOptions.IgnoreTlog,
		UseSignedTimestamps:          opts.CommonVerifyOptions.UseSignedTimestamps,
		TrustedRootPath:              trustedRootPath,
		HashAlgorithm:                hashAlgorithm,
	}

	l.Debug("verifying blob with cosign",
		"key", opts.Key,
		"signature", opts.Signature,
		"bundlePath", opts.BundlePath,
		"offline", opts.CommonVerifyOptions.Offline)

	if err := cmd.Exec(ctx, blobPath); err != nil {
		return err
	}

	l.Debug("blob signature verified successfully")
	return nil
}
