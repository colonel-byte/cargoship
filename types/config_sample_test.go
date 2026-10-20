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

package types

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// sampleConfigPath is the sample config at the repository root, two levels up from this
// package. See the relative-path note in AGENTS.md if either end of this path moves.
const sampleConfigPath = "../cargoship-config.yaml"

// sampleKey matches a config key in the sample, whether it is live or commented out --
// a commented key is how the sample carries a setting that should not be on by default.
var sampleKey = regexp.MustCompile(`(?m)^[\t #]*([a-z0-9_]+):`)

// unreadSettings are config fields nothing in the tree reads, so the sample deliberately
// does not advertise them. `.distro.deploy.retries` is the only one: every --retries flag
// reads DistroOpts.Retry (`.distro.retry`) instead, which leaves DistroDeployOptions a
// section an operator can write and no command will honour. Fix the field or delete it
// rather than lengthening this map.
var unreadSettings = map[string]bool{"retries": true}

// TestSampleConfigNamesEverySetting fails when a field in DistroConfig has no key in
// cargoship-config.yaml. The sample is what an operator copies, so a key missing from it
// is a setting nobody discovers, and nothing else in the build notices the omission --
// the generated schema is complete whether the sample is or not.
func TestSampleConfigNamesEverySetting(t *testing.T) {
	sample, err := os.ReadFile(sampleConfigPath)
	if err != nil {
		t.Fatalf("reading %s: %v", sampleConfigPath, err)
	}

	present := map[string]bool{}
	for _, m := range sampleKey.FindAllStringSubmatch(string(sample), -1) {
		present[m[1]] = true
	}

	for _, key := range settingKeys(reflect.TypeOf(DistroConfig{})) {
		if !present[key] && !unreadSettings[key] {
			t.Errorf("%s names no %q key; add it, commented out where it should not be on by default", sampleConfigPath, key)
		}
	}
}

// settingKeys walks a config struct and returns the json key of every leaf field. A
// nested struct contributes its children rather than itself, so an empty one
// (ApplyOptions, ResetOptions) contributes nothing and is not demanded of the sample.
func settingKeys(t reflect.Type) []string {
	var keys []string
	for i := range t.NumField() {
		field := t.Field(i)
		key := strings.Split(field.Tag.Get("json"), ",")[0]
		if key == "" || key == "-" {
			continue
		}
		if field.Type.Kind() == reflect.Struct {
			keys = append(keys, settingKeys(field.Type)...)
			continue
		}
		keys = append(keys, key)
	}
	return keys
}
