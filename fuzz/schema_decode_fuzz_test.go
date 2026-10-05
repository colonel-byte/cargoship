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

package fuzz

import (
	"testing"

	"github.com/colonel-byte/cargoship/pkg/schema"
)

// FuzzSchemaDecodeNoPanic asserts that Decode never panics on arbitrary YAML bytes.
//
// Every cluster configuration, inventory, and package manifest is checked against its schema by
// going through Decode first: a YAML unmarshal into `any`, round-tripped through JSON to settle the
// document into the six types JSON Schema understands. That round trip is where a YAML value with
// no JSON equivalent -- a non-string map key, an integer larger than a float64 can hold exactly, a
// timestamp -- has to be handled by json.Marshal rather than by anything this package wrote, and the
// bytes reaching it come straight from a file on disk with no shape assumed yet.
func FuzzSchemaDecodeNoPanic(f *testing.F) {
	f.Add([]byte("kind: ZarfCluster\n"))
	f.Add([]byte(""))
	f.Add([]byte("1: one\ntrue: yes\n"))
	f.Add([]byte("a: 99999999999999999999999999999999\n"))
	f.Add([]byte("a: !!binary aGVsbG8=\n"))
	f.Add([]byte("a: 2024-01-01T00:00:00Z\n"))
	f.Add([]byte("- 1\n- 2\n"))
	f.Add([]byte("just a scalar"))
	f.Add([]byte("a: &x\n  y: 1\nb: *x\n"))
	f.Add([]byte("\xff\xfe not valid utf-8"))
	f.Add([]byte("? [a, b]\n: c\n"))

	f.Fuzz(func(_ *testing.T, doc []byte) {
		_, _ = schema.Decode(doc) //nolint:errcheck // no-panic fuzz test, malformed input is expected to error
	})
}
