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
	"fmt"
	"net/url"
	"path"
)

const (
	// OCIURLPrefix the prefix to indicate if a url is an OCI artifact
	OCIURLPrefix = "oci://"
)

// IsURL is a helper function to check if a URL is valid.
func IsURL(source string) bool {
	parsedURL, err := url.Parse(source)
	return err == nil && parsedURL.Scheme != "" && parsedURL.Host != ""
}

// IsOCIURL returns true if the given URL is an OCI URL.
func IsOCIURL(source string) bool {
	parsedURL, err := url.Parse(source)
	return err == nil && parsedURL.Scheme == "oci"
}

// ExtractBasePathFromURL returns filename from URL string
func ExtractBasePathFromURL(urlStr string) (string, error) {
	if !IsURL(urlStr) {
		return "", fmt.Errorf("%s is not a valid URL", urlStr)
	}
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}

	filename := path.Base(parsedURL.Path)
	return filename, nil
}
