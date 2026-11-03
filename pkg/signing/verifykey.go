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

package signing

import (
	"crypto"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	sgoverify "github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/sigstore/sigstore/pkg/signature"
)

// VerifyBlobWithKey verifies a Sigstore bundle over blobPath against a PEM-encoded
// public key, using sigstore-go rather than cosign's CLI command structs.
//
// This is the air-gapped case and only that case: a key the operator holds, a bundle
// beside the artifact, no Fulcio certificate, no transparency log lookup and no
// timestamp authority. Nothing here reaches the network.
//
// It is deliberately not a replacement for CosignVerifyBlobWithOptions, which still
// covers the certificate and keyless paths. See docs/agent/choice-signing-fork.md.
func VerifyBlobWithKey(publicKeyPath, bundlePath, blobPath string) (err error) {
	pubBytes, err := readKeyMaterial(publicKeyPath)
	if err != nil {
		return fmt.Errorf("reading the public key: %w", err)
	}
	pub, err := cryptoutils.UnmarshalPEMToPublicKey(pubBytes)
	if err != nil {
		return fmt.Errorf("parsing the public key: %w", err)
	}
	verifier, err := signature.LoadVerifier(pub, crypto.SHA256)
	if err != nil {
		return fmt.Errorf("building a verifier for the public key: %w", err)
	}

	// A bare key carries no validity window, so it is trusted for all time. The key
	// hint in the bundle is not checked against anything: the operator named the key
	// on the command line, and that is the trust decision.
	trusted := root.NewTrustedPublicKeyMaterial(func(_ string) (root.TimeConstrainedVerifier, error) {
		return root.NewExpiringKey(verifier, time.Time{}, time.Time{}), nil
	})

	b, err := bundle.LoadJSONFromPath(bundlePath)
	if err != nil {
		return fmt.Errorf("reading the signature bundle: %w", err)
	}

	// WithNoObserverTimestamps is what makes this work offline. A key-signed bundle
	// built with no transparency log and no timestamp authority has nothing to
	// observe, and requiring an observer would reject it.
	v, err := sgoverify.NewVerifier(trusted, sgoverify.WithNoObserverTimestamps())
	if err != nil {
		return fmt.Errorf("building the bundle verifier: %w", err)
	}

	blob, err := os.Open(blobPath)
	if err != nil {
		return fmt.Errorf("opening the signed artifact: %w", err)
	}
	defer func() {
		if cerr := blob.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	if _, err := v.Verify(b, sgoverify.NewPolicy(sgoverify.WithArtifact(blob), sgoverify.WithKey())); err != nil {
		return errors.Join(errors.New("signature verification failed"), err)
	}
	return nil
}
