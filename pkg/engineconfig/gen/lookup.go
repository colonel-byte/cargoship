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

package gen

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// Entry holds the zero-value generated Server/Agent config structs for one distro/version, as
// produced by mage generate:engineConfig. Registry (in the generated zz_registry.go) is keyed
// by distro id and minor-version package name (e.g. "v1_35").
type Entry struct {
	Server any
	Agent  any
	// Addons is the set of packaged components this distro/version accepts in `disable:`.
	// Empty when no component source was pulled for it -- consumers must treat empty as "no
	// data", not as "nothing is valid".
	Addons []string
	// CNIs and IngressControllers are the values `cni:` and `ingress-controller:` accept.
	// Both are empty for distros that declare no such flag (k3s).
	CNIs               []string
	IngressControllers []string
}

var minorVersionPattern = regexp.MustCompile(`(\d+)\.(\d+)`)

// Lookup returns the Entry registered for distroID (e.g. "k3s", "rke2") and an engine version
// string in any format containing a dotted major.minor (e.g. "1.35.3-k3s1", "v1.35.3+k3s1").
// The version is truncated to its minor release before lookup, the same way
// mage generate:pullEngineSource truncates upstream tags before pulling source -- so a single
// generated version covers every patch release in that minor line. Reports false if no source
// was ever pulled/generated for that distro/minor-version pair.
func Lookup(distroID, version string) (Entry, bool) {
	pkg, ok := minorPackageName(version)
	if !ok {
		return Entry{}, false
	}
	versions, ok := Registry[distroID]
	if !ok {
		return Entry{}, false
	}
	entry, ok := versions[pkg]
	return entry, ok
}

func minorPackageName(version string) (string, bool) {
	m := minorVersionPattern.FindStringSubmatch(version)
	if m == nil {
		return "", false
	}
	return "v" + m[1] + "_" + m[2], true
}

// Keys returns the set of yaml key names declared on a generated config struct (an Entry's
// Server or Agent field), derived from each field's `yaml:"name,omitempty"` tag.
func Keys(v any) map[string]struct{} {
	keys := map[string]struct{}{}
	t := reflect.TypeOf(v)
	if t == nil || t.Kind() != reflect.Struct {
		return keys
	}
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("yaml")
		name, _, _ := strings.Cut(tag, ",")
		if name != "" {
			keys[name] = struct{}{}
		}
	}
	return keys
}

// UnknownAddons returns the entries of a config.yaml `disable:` value that aren't packaged
// components of the distro/version addons came from, sorted. It reports nothing when addons is
// empty -- a version whose component source was never pulled has no vocabulary to check against,
// and treating that as "everything is unknown" would be worse than not checking at all.
//
// disable is the raw decoded value, which is a YAML list in practice but is legal as a bare
// scalar, so both shapes are accepted.
func UnknownAddons(disable any, addons []string) []string {
	if len(addons) == 0 {
		return nil
	}

	known := make(map[string]struct{}, len(addons))
	for _, a := range addons {
		known[a] = struct{}{}
	}

	var unknown []string
	for _, v := range disableValues(disable) {
		if _, ok := known[v]; !ok {
			unknown = append(unknown, v)
		}
	}
	slices.Sort(unknown)
	return slices.Compact(unknown)
}

// disableValues normalizes a decoded `disable:` value into the component names it names.
func disableValues(disable any) []string {
	switch v := disable.(type) {
	case string:
		return []string{v}
	case []string:
		return v
	case []any:
		values := make([]string, 0, len(v))
		for _, el := range v {
			if s, ok := el.(string); ok {
				values = append(values, s)
			}
		}
		return values
	default:
		return nil
	}
}
