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

// Package utils is for commonly used functions
package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	goyaml "github.com/goccy/go-yaml"
	"github.com/zarf-dev/zarf/src/pkg/state"
)

// ReadByteStrict reads a byte array and tries to place it in the provided "destConfig" object.
//
// An unknown key is an error. This reads the cargoship config file, and a misspelled key that is
// quietly dropped means the setting an operator wrote never takes effect -- which on an air-gapped
// management node is discovered after the run, not during it. It previously warned and re-parsed
// leniently, and because the warning went through a throwaway logger built before the real one
// existed, in practice nothing was printed at all.
//
// goccy annotates the returned error with the line and column, so the caller has somewhere to
// point the operator.
func ReadByteStrict(data []byte, destConfig any) error {
	// destConfig, not &destConfig: the callers already pass a pointer, and taking the address of
	// the interface variable handed goccy a **T to fill. It filled the interface instead of the
	// struct behind it, so the destination stayed at its zero value and Strict() had no fields to
	// compare against -- which is why an unknown key never produced an error.
	if err := goyaml.UnmarshalWithOptions(data, destConfig, goyaml.Strict()); err != nil {
		return fmt.Errorf("failed to unmarshal YAML: %w", err)
	}

	return nil
}

// IdentifySource returns the source type for the given source string.
func IdentifySource(src string) (string, error) {
	if parsed, err := url.Parse(src); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return parsed.Scheme, nil
	}
	if strings.HasSuffix(src, ".tar.zst") || strings.HasSuffix(src, ".tar") {
		return "tarball", nil
	}
	if strings.Contains(src, ".part000") {
		return "split", nil
	}
	// match deployed package names: lowercase, digits, hyphens
	if state.DeployedPackageNameRegex(src) {
		return "cluster", nil
	}
	return "", fmt.Errorf("unknown source %s", src)
}

// RandomString returns a string of hexadecimal characters of a given length that is cryptographically secure, or an error
func RandomString(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
