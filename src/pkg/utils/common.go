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

// Package utils is for commonly used functions
package utils

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	goyaml "github.com/goccy/go-yaml"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/state"
)

// ReadYAMLStrict reads a YAML file into a struct, with strict parsing
func ReadYAMLStrict(path string, destConfig any) error {
	log, err := logger.New(logger.ConfigDefault())
	if err != nil {
		return fmt.Errorf("failed to create logger: %v", err)
	}
	log.Debug("Reading YAML", "path", path)

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file at %s: %v", path, err)
	}
	defer file.Close()

	// First try with strict mode
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("failed to read file at %s: %v", path, err)
	}

	return ReadByteStrict(fileBytes, &destConfig)
}

func ReadByteStrict(data []byte, destConfig any) error {
	log, err := logger.New(logger.ConfigDefault())
	if err != nil {
		return fmt.Errorf("failed to create logger: %v", err)
	}

	err = goyaml.UnmarshalWithOptions(data, &destConfig, goyaml.Strict())
	if err != nil {
		log.Warn("failed strict unmarshalling of YAML", "error", err)

		// Try again with non-strict mode
		err = goyaml.UnmarshalWithOptions(data, &destConfig)
		if err != nil {
			return fmt.Errorf("failed to unmarshal YAML at %v", err)
		}
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

func PrintJSON(obj any) string {
	bytes, _ := json.MarshalIndent(obj, "", "  ")
	return string(bytes)
}

func RandomString(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
