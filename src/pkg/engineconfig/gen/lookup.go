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
	"strings"
)

// Entry holds the zero-value generated Server/Agent config structs for one distro/version, as
// produced by mage generate:engineConfig. Registry (in the generated zz_registry.go) is keyed
// by distro id and minor-version package name (e.g. "v1_35").
type Entry struct {
	Server any
	Agent  any
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
