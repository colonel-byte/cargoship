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

package helmvalues

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/colonel-byte/cargoship/pkg/utils"
	"gopkg.in/yaml.v3"
)

// valuesFileExts are the extensions a values file may carry. ZEP-0021 makes
// values files YAML only, even though a cargoship config file may be written in
// other formats: a values file is read for its types, and restricting the
// format keeps one decoder - and so one set of typing rules - in play.
var valuesFileExts = []string{".yaml", ".yml"}

// ParseFile reads one YAML values file from disk.
//
// An empty file yields an empty map rather than an error, so a placeholder
// checked into a repository is not a build failure. A file whose top level is
// anything but a mapping is rejected: values are addressed by key, and a
// document that is a list or a bare scalar has no keys to address.
func ParseFile(path string) (map[string]any, error) {
	if err := checkValuesExt(path); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading values file: %w", err)
	}
	return parseValues(b, path)
}

// parseValues decodes YAML values, naming src in any error so the caller knows
// which file was at fault.
func parseValues(b []byte, src string) (map[string]any, error) {
	var raw any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("parsing values file %s: %w", src, err)
	}
	if raw == nil {
		// An empty document, or a file holding only comments.
		return map[string]any{}, nil
	}
	m, ok := asMap(raw)
	if !ok {
		return nil, fmt.Errorf("values file %s must hold a mapping at its top level, got %T", src, raw)
	}
	return normalizeMap(m), nil
}

// checkValuesExt reports whether p names a YAML file. The check is on the
// extension rather than the content because a mistyped path that happens to
// parse as YAML - a README, a script - would otherwise be merged into the
// values silently.
func checkValuesExt(p string) error {
	ext := strings.ToLower(filepath.Ext(p))
	for _, want := range valuesFileExts {
		if ext == want {
			return nil
		}
	}
	return fmt.Errorf("values file %s must be YAML (%s)", p, strings.Join(valuesFileExts, " or "))
}

// LoadFiles reads and merges every values file in paths, in order.
//
// A later file wins over an earlier one, key by key, which is the order
// `helm -f a.yaml -f b.yaml` resolves in and what ZEP-0021 specifies when two
// sources set the same path.
//
// A path may be a URL or a local file. A relative local path resolves against
// baseDir, so a package's values files are named relative to the package
// definition rather than to the directory cargoship was invoked from.
//
// A URL is fetched fresh into tmpDir rather than served from the shared cache: a
// values file is configuration that an author edits in place, and a cached copy
// would silently build the package from values that have since changed. When
// tmpDir is empty a temporary directory is created and removed before returning;
// a caller that needs the downloaded copies afterwards passes its own directory,
// or uses ResolveFiles.
func LoadFiles(ctx context.Context, baseDir, tmpDir string, paths []string) (_ map[string]any, err error) {
	merged := map[string]any{}
	if len(paths) == 0 {
		return merged, nil
	}

	if tmpDir == "" {
		tmpDir, err = os.MkdirTemp("", "cargoship-values-")
		if err != nil {
			return nil, fmt.Errorf("creating temp dir for values files: %w", err)
		}
		defer func() {
			err = errors.Join(err, os.RemoveAll(tmpDir))
		}()
	}

	local, err := ResolveFiles(ctx, baseDir, tmpDir, paths)
	if err != nil {
		return nil, err
	}
	for _, p := range local {
		vals, err := ParseFile(p)
		if err != nil {
			return nil, err
		}
		merged = MergeValues(merged, vals)
	}
	return merged, nil
}

// ResolveFiles turns a list of values file references into readable local
// paths, in the same order, downloading any that are URLs into tmpDir.
//
// It is separate from LoadFiles so a caller that has to do something with the
// files themselves - copy them into a package, say - works from the same
// resolution rules as a caller that only wants the merged values. tmpDir is
// required here, since the downloaded files outlive the call.
func ResolveFiles(ctx context.Context, baseDir, tmpDir string, paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for i, p := range paths {
		local, err := localValuesPath(ctx, baseDir, tmpDir, i, p)
		if err != nil {
			return nil, err
		}
		out = append(out, local)
	}
	return out, nil
}

// localValuesPath resolves one entry of a values file list to a readable local
// path, downloading it first when it is a URL. idx makes the download name
// unique, so two URLs ending in values.yaml do not overwrite each other.
func localValuesPath(ctx context.Context, baseDir, tmpDir string, idx int, p string) (string, error) {
	if !helpers.IsURL(p) {
		if filepath.IsAbs(p) {
			return p, nil
		}
		return filepath.Join(baseDir, p), nil
	}

	name, err := urlFileName(p)
	if err != nil {
		return "", err
	}
	// The extension is checked before the download so a wrong URL costs nothing.
	if err := checkValuesExt(name); err != nil {
		return "", err
	}
	dst := filepath.Join(tmpDir, fmt.Sprintf("%d-%s", idx, name))
	if err := utils.DownloadToFile(ctx, p, dst); err != nil {
		return "", fmt.Errorf("downloading values file %s: %w", p, err)
	}
	return dst, nil
}

// urlFileName returns the last path element of a URL, which is the name the
// downloaded copy is given. A URL with no path element to take a name from is
// rejected rather than guessed at.
func urlFileName(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parsing values file URL %s: %w", raw, err)
	}
	name := path.Base(u.Path)
	if name == "." || name == "/" || name == "" {
		return "", fmt.Errorf("values file URL %s has no file name", raw)
	}
	return name, nil
}
