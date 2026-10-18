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

package signing

import (
	"fmt"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
)

// SigningMethod identifies how a Sigstore bundle was signed.
type SigningMethod string

const (
	// SigningMethodKeyless indicates a Fulcio-issued certificate bundle (OIDC/keyless).
	SigningMethodKeyless SigningMethod = "keyless"
	// SigningMethodKey indicates a public-key bundle.
	SigningMethodKey SigningMethod = "key"
)

// BundleInfo contains parsed metadata from a Sigstore bundle file.
type BundleInfo struct {
	Method           SigningMethod
	Identity         string // cert SAN — empty for key-based signatures
	Issuer           string // OIDC issuer — empty for key-based signatures
	HasTSATimestamps bool   // true if the bundle contains signed timestamps
}

// ReadBundleInfo parses a Sigstore bundle file and returns its signing metadata.
func ReadBundleInfo(bundlePath string) (BundleInfo, error) {
	b, err := bundle.LoadJSONFromPath(bundlePath)
	if err != nil {
		return BundleInfo{}, fmt.Errorf("loading bundle: %w", err)
	}
	timestamps, err := b.Timestamps()
	if err != nil {
		return BundleInfo{}, fmt.Errorf("reading bundle timestamps: %w", err)
	}

	vc, err := b.VerificationContent()
	if err != nil {
		return BundleInfo{}, fmt.Errorf("reading verification content: %w", err)
	}
	switch v := vc.(type) {
	case *bundle.Certificate:
		summary, err := certificate.SummarizeCertificate(v.Certificate())
		if err != nil {
			return BundleInfo{}, fmt.Errorf("reading certificate identity: %w", err)
		}
		return BundleInfo{Method: SigningMethodKeyless, Identity: summary.SubjectAlternativeName, Issuer: summary.Extensions.Issuer, HasTSATimestamps: len(timestamps) > 0}, nil
	case *bundle.PublicKey:
		return BundleInfo{Method: SigningMethodKey, HasTSATimestamps: len(timestamps) > 0}, nil
	default:
		return BundleInfo{}, fmt.Errorf("unrecognised verification content type %T", vc)
	}
}
