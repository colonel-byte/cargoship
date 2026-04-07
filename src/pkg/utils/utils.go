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

package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	goyaml "github.com/goccy/go-yaml"
	"github.com/zarf-dev/zarf/src/pkg/logger"
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

func PrintJSON(obj any) string {
	bytes, _ := json.MarshalIndent(obj, "", "  ")
	return string(bytes)
}
