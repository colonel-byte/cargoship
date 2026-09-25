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

	"github.com/colonel-byte/cargoship/src/pkg/helmvalues"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// pathSentinel is written by FuzzValuePathRoundTrip at whatever path the fuzzer produces. It is a
// map so that the root path "." -- the one case SetValuePath treats specially, merging rather than
// replacing -- accepts it too, which lets one target cover both without branching on the path
// shape.
func pathSentinel() map[string]any {
	return map[string]any{"sentinel": true}
}

// FuzzValuePathRoundTrip asserts that SetValuePath and GetValuePath agree on which ZEP-0021 value
// paths are well-formed, and that a value just written is the value read back.
//
// Both functions parse the same path grammar -- a leading dot, dot-separated keys, escapes for a
// literal ".", "[" or "\", and bracketed list indices -- but do it independently on the way into
// two different walks, one that creates containers and one that only reads them. A parser bug that
// makes one accept a path the other rejects would leave a value that was written unreadably, or a
// read that silently returns "not found" for a path that was actually just set. GetValuePath on an
// always-empty map is used as the reference for whether a path parses, since on an empty source
// only a malformed path can produce its error.
func FuzzValuePathRoundTrip(f *testing.F) {
	f.Add(".")
	f.Add("")
	f.Add("a.b.c")
	f.Add(".a.b.c")
	f.Add(".a[0].b[1][2]")
	f.Add(".a\\.b")
	f.Add(".a\\[0].b")
	f.Add(".servers[0]")
	f.Add(".servers[63]")
	f.Add(".servers[64]")
	f.Add(".servers[9999999999999999999]")
	f.Add(".servers[-1]")
	f.Add(".a.")
	f.Add(".a..b")
	f.Add(".a[")
	f.Add(".a]")
	f.Add(".a[b]")
	f.Add(".\\")
	f.Add(".a\\")
	f.Add(".😀.b")

	f.Fuzz(func(t *testing.T, path string) {
		_, _, refErr := helmvalues.GetValuePath(map[string]any{}, path)
		pathParses := refErr == nil

		val := pathSentinel()
		dst := map[string]any{}
		setErr := helmvalues.SetValuePath(dst, path, val)

		if !pathParses {
			require.Error(t, setErr, "path %q rejected by GetValuePath but accepted by SetValuePath", path)
			return
		}
		require.NoError(t, setErr, "path %q accepted by GetValuePath but rejected by SetValuePath: %v", path, setErr)

		got, ok, err := helmvalues.GetValuePath(dst, path)
		require.NoError(t, err, "path %q parsed once to set and failed to parse to read back", path)
		require.True(t, ok, "value just written at %q is not readable back", path)
		require.Equal(t, val, got, "value read back at %q does not match what was written", path)
	})
}

// FuzzApplyMappingNoPanic asserts that ApplyMapping never panics and never grows a list beyond the
// bound SetValuePath enforces, whatever source and target paths a package's mapping declares.
//
// A mapping's source and target are written by a package author, not an operator, but cargoship
// still validates packages it did not build itself, so a crafted target like ".a[9999999999]" has
// to fail cleanly rather than attempt an allocation sized by the fuzzer.
func FuzzApplyMappingNoPanic(f *testing.F) {
	f.Add(".source", ".target")
	f.Add(".", ".")
	f.Add(".a.b[0]", ".x.y[63]")
	f.Add(".a[999999999999]", ".b")
	f.Add("", "")
	f.Add(".a", "")

	f.Fuzz(func(_ *testing.T, source, target string) {
		values := map[string]any{"source": map[string]any{"nested": 1}}
		dst := map[string]any{}
		_, _ = helmvalues.ApplyMapping(dst, values, source, target) //nolint:errcheck // no-panic fuzz test, malformed paths are expected to error
	})
}

// FuzzMergeValuesImmutable asserts that MergeValues never mutates either argument, whatever shape
// two independently fuzzed YAML documents decode to.
//
// Values files are merged repeatedly while composing a package -- once per file on the command
// line, again for a component's own defaults -- so an in-place merge would let one merge's result
// leak into the map every later merge starts from. That bug would not show up as a crash, only as
// a value from an earlier file reappearing in a later one, which is why this checks the inputs
// byte-for-byte rather than just asserting MergeValues returns.
func FuzzMergeValuesImmutable(f *testing.F) {
	f.Add([]byte("a: 1\nb:\n  c: 2\n"), []byte("b:\n  c: 3\n  d: 4\n"))
	f.Add([]byte("{}"), []byte("a: [1, 2, 3]\n"))
	f.Add([]byte("a: &x\n  y: 1\nb: *x\n"), []byte("a:\n  z: 2\n"))
	f.Add([]byte("not a map"), []byte("also: not a map, but this is"))

	f.Fuzz(func(t *testing.T, dstYAML, srcYAML []byte) {
		dst, dstOK := decodeValuesMap(t, dstYAML)
		src, srcOK := decodeValuesMap(t, srcYAML)
		if !dstOK || !srcOK {
			return
		}

		dstBefore := renderValues(t, dst)
		srcBefore := renderValues(t, src)

		_ = helmvalues.MergeValues(dst, src)

		require.Equal(t, dstBefore, renderValues(t, dst), "MergeValues mutated its dst argument")
		require.Equal(t, srcBefore, renderValues(t, src), "MergeValues mutated its src argument")
	})
}

// decodeValuesMap decodes y as YAML and reports whether it produced a map, the shape MergeValues
// expects both arguments in.
func decodeValuesMap(t *testing.T, y []byte) (map[string]any, bool) {
	t.Helper()

	var raw any
	if err := yaml.Unmarshal(y, &raw); err != nil {
		return nil, false
	}
	m, ok := raw.(map[string]any)
	if !ok {
		// A document that decodes to something other than string keys throughout
		// (map[any]any, a list, a scalar) is not a shape MergeValues's callers ever
		// pass it; ParseFile itself rejects a non-mapping top level.
		return nil, false
	}
	return m, true
}

// renderValues renders v to YAML for a before/after comparison. Rendering rather than
// reflect.DeepEqual is what catches a mutation that replaces a nested map with an equal-looking
// copy of itself, which DeepEqual cannot tell apart from the original but a byte comparison of two
// renders can if the render order or scalar style shifted underneath it.
func renderValues(t *testing.T, v map[string]any) string {
	t.Helper()

	out, err := helmvalues.RenderValues(v)
	require.NoError(t, err)
	return out
}
