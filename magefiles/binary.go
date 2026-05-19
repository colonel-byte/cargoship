// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/magefile/mage/mg"
	"github.com/uwu-tools/magex/pkg/archive"
	"github.com/uwu-tools/magex/pkg/downloads"
	"github.com/uwu-tools/magex/pkg/gopath"
)

type (
	Binary mg.Namespace
)

// Dagger install dagger into gopath
func (Binary) Dagger() error {
	return ensureDagger()
}

func binInPath(bin string) bool {
	_, err := os.Stat(binaryPath(bin))
	return !errors.Is(err, os.ErrNotExist)
}

func binaryPath(bin string) string {
	return filepath.Join(gopath.GOPATH(), "bin", bin)
}

func ensureDagger() error {
	if !binInPath("dagger") {
		return archive.DownloadToGopathBin(
			archive.DownloadArchiveOptions{
				DownloadOptions: downloads.DownloadOptions{
					Name:        "dagger",
					Version:     "0.20.8",
					UrlTemplate: "https://dl.dagger.io/dagger/releases/{{.VERSION}}/dagger_v{{.VERSION}}_{{.GOOS}}_{{.GOARCH}}{{.EXT}}",
				},
				ArchiveExtensions: map[string]string{
					"linux":   ".tar.gz",
					"darwin":  ".tar.gz",
					"windows": ".zip",
				},
			},
		)
	}
	return nil
}
