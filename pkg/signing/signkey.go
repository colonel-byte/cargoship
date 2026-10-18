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
	"context"
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/sigstore/cosign/v3/pkg/cosign"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	"github.com/sigstore/sigstore-go/pkg/sign"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/sigstore/sigstore/pkg/signature"
	"google.golang.org/protobuf/encoding/protojson"
)

// cosignKeypair adapts a cosign-format private key to sigstore-go's Keypair,
// so a bundle can be signed without cosign's CLI command structs.
type cosignKeypair struct {
	signer crypto.Signer
	hint   []byte
}

// newCosignKeypair loads an encrypted cosign private key, the format
// `cosign generate-key-pair` writes.
func newCosignKeypair(keyPath, password string) (*cosignKeypair, error) {
	keyBytes, err := readKeyMaterial(keyPath)
	if err != nil {
		return nil, fmt.Errorf("reading the private key: %w", err)
	}
	sv, err := loadCosignPrivateKey(keyBytes, []byte(password))
	if err != nil {
		return nil, err
	}
	pub, err := sv.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("reading the public half of the private key: %w", err)
	}
	signer, ok := sv.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("the private key in %s does not implement crypto.Signer", keyPath)
	}
	// The hint identifies which key signed the bundle. It is the base64 of the
	// SHA-256 of the DER public key, and it is carried as the bytes of that base64
	// text rather than the raw digest, because the field is a proto string and
	// protojson rejects a digest that is not valid UTF-8. This matches what
	// sigstore-go's own EphemeralKeypair writes.
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("encoding the public key: %w", err)
	}
	sum := sha256.Sum256(der)
	hint := []byte(base64.StdEncoding.EncodeToString(sum[:]))
	return &cosignKeypair{signer: signer, hint: hint}, nil
}

func (k *cosignKeypair) GetHashAlgorithm() protocommon.HashAlgorithm {
	return protocommon.HashAlgorithm_SHA2_256
}

func (k *cosignKeypair) GetSigningAlgorithm() protocommon.PublicKeyDetails {
	return protocommon.PublicKeyDetails_PKIX_ECDSA_P256_SHA_256
}

func (k *cosignKeypair) GetHint() []byte { return k.hint }

func (k *cosignKeypair) GetKeyAlgorithm() string { return "ECDSA" }

func (k *cosignKeypair) GetPublicKey() crypto.PublicKey { return k.signer.Public() }

func (k *cosignKeypair) GetPublicKeyPem() (string, error) {
	pem, err := cryptoutils.MarshalPublicKeyToPEM(k.signer.Public())
	if err != nil {
		return "", err
	}
	return string(pem), nil
}

// SignData signs the SHA-256 digest of data and returns the signature alongside
// the digest, which is the contract sigstore-go's Keypair expects.
func (k *cosignKeypair) SignData(_ context.Context, data []byte) ([]byte, []byte, error) {
	digest := sha256.Sum256(data)
	sig, err := k.signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		return nil, nil, err
	}
	return sig, digest[:], nil
}

// SignBlobWithKey signs blobPath with a cosign-format private key and writes a
// Sigstore bundle to bundlePath, using sigstore-go rather than cosign's CLI.
//
// No transparency log, no timestamp authority and no Fulcio certificate: the bundle
// carries the public key and the signature, which is all an air-gapped verifier has
// to work with. Nothing here reaches the network.
func SignBlobWithKey(ctx context.Context, keyPath, password, blobPath, bundlePath string) error {
	keypair, err := newCosignKeypair(keyPath, password)
	if err != nil {
		return err
	}
	blob, err := os.ReadFile(blobPath)
	if err != nil {
		return fmt.Errorf("reading the artifact to sign: %w", err)
	}

	b, err := sign.Bundle(&sign.PlainData{Data: blob}, keypair, sign.BundleOptions{Context: ctx})
	if err != nil {
		return fmt.Errorf("signing the artifact: %w", err)
	}

	raw, err := protojson.Marshal(b)
	if err != nil {
		return fmt.Errorf("encoding the signature bundle: %w", err)
	}
	// protojson's output is not stable across builds, so it is normalised through
	// encoding/json to keep a reproducible build reproducible.
	var canonical any
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return fmt.Errorf("re-reading the signature bundle: %w", err)
	}
	out, err := json.Marshal(canonical)
	if err != nil {
		return fmt.Errorf("re-encoding the signature bundle: %w", err)
	}
	if err := os.WriteFile(bundlePath, out, 0o644); err != nil { //nolint:gosec // a signature bundle is public
		return fmt.Errorf("writing the signature bundle: %w", err)
	}
	return nil
}

// loadCosignPrivateKey is the one call into cosign's library packages that the
// key-only path keeps: the encrypted private-key envelope is cosign's format, and
// re-deriving its scrypt and secretbox parameters here would be a way to get them
// subtly wrong.
func loadCosignPrivateKey(keyBytes, password []byte) (signature.SignerVerifier, error) {
	sv, err := cosignLoadPrivateKey(keyBytes, password)
	if err != nil {
		return nil, fmt.Errorf("loading the private key (wrong password?): %w", err)
	}
	return sv, nil
}

// cosignLoadPrivateKey is indirected so the key-only path has one named seam onto
// cosign, rather than the call being buried in the middle of a function.
func cosignLoadPrivateKey(keyBytes, password []byte) (signature.SignerVerifier, error) {
	return cosign.LoadPrivateKey(keyBytes, password, nil)
}
