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

// Package helpers are a sub-section of function from defense-unicorn pkg helpers package
package helpers

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/otiai10/copy"
)

// CreateDirectory creates a directory for the given path and file mode.
func CreateDirectory(path string, mode os.FileMode) error {
	if InvalidPath(path) {
		return os.MkdirAll(path, mode)
	}
	return nil
}

// CreateFile creates an empty file at the given path.
func CreateFile(filepath string) error {
	if InvalidPath(filepath) {
		f, err := os.Create(filepath)
		f.Close() //nolint:errcheck
		return err
	}

	return nil
}

// InvalidPath checks if the given path is valid (if it is a permissions error it is there we just don't have access)
func InvalidPath(path string) bool {
	_, err := os.Stat(path)
	return !os.IsPermission(err) && err != nil
}

// CreateParentDirectory creates the parent directory for the given file path.
func CreateParentDirectory(destination string) error {
	parentDest := filepath.Dir(destination)
	return CreateDirectory(parentDest, ReadWriteExecuteUser)
}

// CreatePathAndCopy creates the parent directory for the given file path and copies the source file to the destination.
func CreatePathAndCopy(source string, destination string) error {
	if err := CreateParentDirectory(destination); err != nil {
		return err
	}

	// Copy all the source data into the destination location
	if err := copy.Copy(source, destination); err != nil {
		return err
	}

	// If the path doesn't exist yet then this is an empty file and we should create it
	return CreateFile(destination)
}

// IsDir returns true if the given path is a directory.
func IsDir(path string) bool {
	info, err := os.Stat(filepath.Clean(path))
	if os.IsNotExist(err) {
		return false
	}
	return info.IsDir()
}

// GetSHA256OfFile returns the SHA256 hash of the provided file.
func GetSHA256OfFile(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck

	return GetSHA256Hash(f)
}

// SHAsMatch returns an error if the SHA256 hash of the provided file does not match the expected hash.
func SHAsMatch(path, expected string) error {
	sha, err := GetSHA256OfFile(path)
	if err != nil {
		return err
	}
	if sha != expected {
		return fmt.Errorf("expected sha256 of %s to be %s, found %s", path, expected, sha)
	}
	return nil
}
