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

// Package fileoverride redirects the file downloads a distro definition declares to an
// internal mirror or to a directory of pre-staged assets, the way images.RegistryOverride
// redirects image pulls.
//
// It differs from the registry case in one way worth knowing. A registry override matches on
// a raw string prefix, which is survivable for a registry reference but not for a URL: an
// override for "https://github.com" would otherwise also capture
// "https://github.com.example.invalid/...". Matching here is done on the parsed URL, and the
// prefix has to end on a path-segment boundary.
package fileoverride

import (
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
)

// Override redirects every file source under Source to Target.
type Override struct {
	// Source is the URL prefix a file source is matched against, for example
	// "https://rpm.rancher.io" or "https://github.com/rancher".
	Source string
	// Target is the replacement as it was written: either an http/https URL prefix, or a
	// path to a local directory of pre-staged files.
	Target string
	// LocalDir is set, and absolute, when Target names a directory rather than a URL. It is
	// resolved once at parse time so a rewritten source can never be taken for a relative
	// path and re-resolved against the distro definition directory.
	LocalDir string
}

// IsLocal reports whether this override redirects to a local directory rather than a URL.
func (o Override) IsLocal() bool {
	return o.LocalDir != ""
}

// Match is a resolved override: which Override applied, and the source to actually read.
type Match struct {
	// Override is the entry that matched.
	Override Override
	// Resolved is the effective source: a rewritten URL, or an absolute local path.
	Resolved string
}

// Parse converts "source=target" pairs into a structured type. The result is sorted in
// descending order by Source, which puts the longest prefix first -- so an override for
// "https://github.com/rancher" wins over a broader "https://github.com" -- matching how
// cmd.ParseRegistryOverrides orders registry overrides for the same reason.
//
// Input is of the following form:
// []string{"https://github.com/rancher=https://mirror.example.com/rancher", "https://rpm.rancher.io=/srv/staged"}
func Parse(overrides []string) ([]Override, error) {
	result := make([]Override, 0, len(overrides))
	for i, mapping := range overrides {
		source, target, found := strings.Cut(mapping, "=")
		if !found {
			return nil, fmt.Errorf("file override missing '=': %s", mapping)
		}
		if source == "" {
			return nil, fmt.Errorf("file override missing source: %s", mapping)
		}
		if target == "" {
			return nil, fmt.Errorf("file override missing value: %s", mapping)
		}
		if index := slices.IndexFunc(result, func(existing Override) bool {
			return existing.Source == source
		}); index >= 0 {
			return nil, fmt.Errorf("file override has duplicate source: existing index %d, new index %d, source %s", index, i, source)
		}

		// A distro definition's file sources are only overridden when they are URLs, so a
		// source that is not one could never match and is far more likely a typo than an
		// intent worth guessing at.
		if _, err := parseHTTPURL(source); err != nil {
			return nil, fmt.Errorf("file override source must be an http or https URL: %s", source)
		}

		o := Override{Source: source, Target: target}
		if _, err := parseHTTPURL(target); err != nil {
			// Not a URL, so it names a directory of pre-staged files.
			if u, perr := url.Parse(target); perr == nil && u.Scheme != "" && len(u.Scheme) > 1 {
				return nil, fmt.Errorf("file override target must be an http or https URL or a local directory path: %s", target)
			}
			abs, aerr := filepath.Abs(target)
			if aerr != nil {
				return nil, fmt.Errorf("file override target %s cannot be resolved to an absolute path: %w", target, aerr)
			}
			o.LocalDir = abs
		}
		result = append(result, o)
	}

	slices.SortFunc(result, func(a Override, b Override) int {
		return -strings.Compare(a.Source, b.Source)
	})

	return result, nil
}

// Resolve returns the effective source for src under the first override that matches it.
// ok is false when no override applies, in which case src should be used unchanged.
//
// A non-URL src never matches: a distro definition that already points at a local file has
// nothing to redirect.
func Resolve(overrides []Override, src string) (Match, bool, error) {
	srcURL, err := parseHTTPURL(src)
	if err != nil {
		return Match{}, false, nil
	}

	for _, o := range overrides {
		// Parse succeeded for every Source at parse time.
		sourceURL, err := parseHTTPURL(o.Source)
		if err != nil {
			continue
		}
		remainder, matched := matchRemainder(srcURL, sourceURL)
		if !matched {
			continue
		}

		if !o.IsLocal() {
			resolved := strings.TrimSuffix(o.Target, "/")
			if remainder != "" {
				resolved += "/" + remainder
			}
			// The query and fragment belong to the file being fetched, not to the host
			// it is fetched from, so they survive the rewrite.
			if srcURL.RawQuery != "" {
				resolved += "?" + srcURL.RawQuery
			}
			if srcURL.Fragment != "" {
				resolved += "#" + srcURL.Fragment
			}
			return Match{Override: o, Resolved: resolved}, true, nil
		}

		// url.Path is percent-decoded, so the remainder can contain ".." however the URL
		// spelled it. Join and then prove the result is still inside the target directory
		// rather than trying to sanitize the input.
		resolved := filepath.Join(o.LocalDir, filepath.FromSlash(remainder))
		rel, err := filepath.Rel(o.LocalDir, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return Match{}, false, fmt.Errorf("file override for %s resolves %s outside the override directory %s", o.Source, src, o.LocalDir)
		}
		return Match{Override: o, Resolved: resolved}, true, nil
	}

	return Match{}, false, nil
}

// ToMap flattens overrides into the source-to-target form the config file and the package
// build metadata both use. Ordering is not preserved, which is why it is not used for matching.
func ToMap(overrides []Override) map[string]string {
	if len(overrides) == 0 {
		return nil
	}
	out := make(map[string]string, len(overrides))
	for _, o := range overrides {
		out[o.Source] = o.Target
	}
	return out
}

// matchRemainder reports whether source is a prefix of src on a path-segment boundary, and
// returns the part of src's path that source did not cover. Scheme and host must be equal;
// a bare host prefix matches every path under it.
func matchRemainder(src *url.URL, source *url.URL) (string, bool) {
	if !strings.EqualFold(src.Scheme, source.Scheme) || !strings.EqualFold(src.Host, source.Host) {
		return "", false
	}

	srcPath := strings.TrimPrefix(src.Path, "/")
	sourcePath := strings.Trim(source.Path, "/")
	if sourcePath == "" {
		return srcPath, true
	}
	if !strings.HasPrefix(srcPath, sourcePath) {
		return "", false
	}
	rest := srcPath[len(sourcePath):]
	if rest == "" {
		return "", true
	}
	// Without this, an override for ".../rancher" would also capture ".../rancher-dev".
	if rest[0] != '/' {
		return "", false
	}
	return strings.TrimPrefix(rest, "/"), true
}

// parseHTTPURL parses s and requires it to be an absolute http or https URL with a host.
func parseHTTPURL(s string) (*url.URL, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("not an http or https URL: %s", s)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("URL has no host: %s", s)
	}
	return u, nil
}
