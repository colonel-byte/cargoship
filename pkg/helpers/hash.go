// Copyright 2024 Defense Unicorns authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from pkg:
// https://github.com/defenseunicorns/pkg
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

package helpers

import (
	"crypto"
	"encoding/hex"
	"io"
)

// GetCryptoHash returns the computed SHA256 Sum of a given file
func GetCryptoHash(data io.ReadCloser, hashName crypto.Hash) (string, error) {
	hash := hashName.New()
	if _, err := io.Copy(hash, data); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// GetSHA256Hash returns the computed SHA256 Sum of a given file
func GetSHA256Hash(data io.ReadCloser) (string, error) {
	return GetCryptoHash(data, crypto.SHA256)
}
