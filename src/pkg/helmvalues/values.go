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

// Package helmvalues renders Helm-style value templates and reads and writes
// the nested interface values those templates draw on.
//
// Values are addressed by ZEP-0021 value paths: a leading dot followed by
// dot-separated keys, with list indices written after a key, as in
// .servers[0].port. A lone "." names the root.
//
// Scalar typing follows Helm's strvals package rather than Go's own parsing
// rules, so a value that travels through cargoship lands in a chart as the same
// type it would have landed as under plain `helm --set`.
package helmvalues

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/colonel-byte/cargoship/src/pkg/utils"
	"github.com/go-sprout/sprout/sprigin"
	"github.com/k0sproject/dig"
	"gopkg.in/yaml.v3"
)

// deniedFuncs are template functions sprout exposes that read host state having
// nothing to do with the values being rendered. A package author writes the
// templates and an operator runs them, so leaving these in place would let a
// package read the operator's environment (tokens, kubeconfig paths) into chart
// values, or resolve names against the operator's DNS. Helm removes the same
// functions from chart templates for the same reason.
//
// Removing rather than stubbing them is deliberate: an unknown function is a
// template *parse* error, so a package that uses one fails loudly instead of
// rendering an empty string. Re-audit this list when sprout is upgraded.
var deniedFuncs = []string{
	"env",
	"expandEnv",
	"expandenv",
	"getHostByName",
}

// Option adjusts which template functions are available to a render.
type Option func(*options)

type options struct {
	createFuncs bool
	ctx         context.Context
}

// WithCreateFuncs exposes the helpers that reach the local filesystem and the
// network: fileExists, cachedFileExists, cachedFilePath and downloadToCache.
//
// These belong to onCreate actions, where pulling a missing artifact into the
// cache is the whole point. Everywhere else - values files, manifests, files,
// and deploy-time actions - leave them off. A template that names one without
// this option fails at parse time with "function ... not defined", which is the
// behaviour we want: rendering a chart's values should not be able to probe the
// deploy host or issue HTTP requests.
//
// ctx governs the downloads downloadToCache performs. It is taken here rather
// than on RenderTemplate because this option is the only thing that makes a
// render capable of I/O at all; a values render without it cannot block on
// anything and has no use for a context.
func WithCreateFuncs(ctx context.Context) Option {
	return func(o *options) {
		o.createFuncs = true
		o.ctx = ctx
	}
}

func newOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// FuncMap returns the sprig-compatible template FuncMap, minus the functions
// that read host state, plus cargoship's own helpers.
//
// The sprig compatibility layer is used rather than a hand-picked set of sprout
// registries because the raw registries register none of the sprig aliases -
// toYaml, upper, b64enc and the rest - that Helm users expect to be able to
// type.
func FuncMap(opts ...Option) template.FuncMap {
	o := newOptions(opts)

	fm := sprigin.FuncMap()
	for _, name := range deniedFuncs {
		delete(fm, name)
	}

	if o.createFuncs {
		addCreateFuncs(o.ctx, fm)
	}

	return fm
}

// addCreateFuncs installs the filesystem- and network-backed helpers. See
// WithCreateFuncs for why they are not part of the default map.
//
// Every one of these returns its error rather than a zero value. text/template
// turns a returned error into an execution error, so a cache directory that
// cannot be resolved stops the render instead of quietly reading as "the file is
// not there" and sending the package down the wrong branch.
func addCreateFuncs(ctx context.Context, fm template.FuncMap) {
	if ctx == nil {
		ctx = context.Background()
	}

	// fileExists checks if a path exists on the local filesystem.
	fm["fileExists"] = statExists

	// cachedFileExists checks if a file exists under cargoship's cache directory (or custom relative cache path).
	fm["cachedFileExists"] = func(relPath string) (bool, error) {
		target, err := cachedPath(relPath)
		if err != nil {
			return false, err
		}
		return statExists(target)
	}

	// cachedFilePath returns the absolute path to a file in the cargoship cache directory.
	fm["cachedFilePath"] = cachedPath

	// downloadToCache downloads a URL into cargoship's cache directory and returns the absolute local path.
	// Optionally takes an expected SHA256 checksum as a 3rd parameter.
	fm["downloadToCache"] = func(url, relPath string, args ...string) (string, error) {
		return utils.DownloadToCache(ctx, url, relPath, args...)
	}
}

// statExists reports whether path exists. An error that is not "does not exist" -
// a permission problem on a parent directory, say - is returned rather than
// reported as absence, so a template cannot take the wrong branch because the
// cache was unreadable.
func statExists(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// cachedPath resolves relPath against cargoship's cache directory.
func cachedPath(relPath string) (string, error) {
	cacheRoot, err := utils.ResolveCachePath("")
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheRoot, relPath), nil
}

// RenderTemplate evaluates a Go template string using sprig-compatible template functions.
func RenderTemplate(tmplStr string, data any, opts ...Option) (string, error) {
	if !strings.Contains(tmplStr, "{{") {
		return tmplStr, nil
	}
	tmpl, err := template.New("values").Funcs(FuncMap(opts...)).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}
	out := buf.String()
	if strings.Contains(out, noValue) {
		return "", fmt.Errorf("template %q resolved a missing key to %q", truncate(tmplStr, 120), noValue)
	}
	return out, nil
}

// noValue is what text/template prints for a map key that does not exist. A typo
// in a value path would otherwise ship the literal string "<no value>" into a
// chart, so it is rejected instead.
//
// The output is checked rather than setting missingkey=error because that option
// also rejects "{{ if .Values.opt }}", "{{ with .Values.opt }}",
// "{{ .Values.opt | default "x" }}" and "{{ empty .Values.opt }}" - the idioms a
// Helm user reaches for to make a value optional. None of those can emit
// <no value>, so checking the output catches only the genuine mistake.
const noValue = "<no value>"

// truncate shortens s for an error message. A template can be a whole manifest,
// and the first line of it is enough to find.
func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "..."
}

// EvaluateValuesTemplates recursively renders template strings inside any map or slice structure.
//
// A string that contained a template is typed afterwards, so
// "{{ .Values.enabled }}" can produce a real chart boolean rather than the
// string "true". Typing uses Helm's scalar rules (see typedScalar) and does not
// interpret list syntax, so rendered output that happens to look like "[a,b]"
// stays a string. A string that contained no template is left exactly as the
// YAML decoder produced it, since it already carries its type.
func EvaluateValuesTemplates(v any, data any, opts ...Option) (any, error) {
	switch val := v.(type) {
	case string:
		rendered, err := RenderTemplate(val, data, opts...)
		if err != nil {
			return nil, err
		}
		if strings.Contains(val, "{{") {
			trimmed := strings.TrimSpace(rendered)
			typed := typedScalar(trimmed)
			if str, ok := typed.(string); ok && str == trimmed {
				// No type was inferred, so hand back what the template actually
				// produced. Returning the trimmed form would eat the trailing
				// newline of a block such as "{{ .Values.resources | toYaml }}".
				return rendered, nil
			}
			return typed, nil
		}
		return rendered, nil
	case dig.Mapping:
		res := make(dig.Mapping, len(val))
		for k, item := range val {
			evaledKey, err := RenderTemplate(k, data, opts...)
			if err != nil {
				return nil, err
			}
			evaledVal, err := EvaluateValuesTemplates(item, data, opts...)
			if err != nil {
				return nil, err
			}
			res[evaledKey] = evaledVal
		}
		return res, nil
	case map[string]any:
		res := make(map[string]any, len(val))
		for k, item := range val {
			evaledKey, err := RenderTemplate(k, data, opts...)
			if err != nil {
				return nil, err
			}
			evaledVal, err := EvaluateValuesTemplates(item, data, opts...)
			if err != nil {
				return nil, err
			}
			res[evaledKey] = evaledVal
		}
		return res, nil
	case map[any]any:
		res := make(map[string]any, len(val))
		for k, item := range val {
			kStr := fmt.Sprint(k)
			evaledKey, err := RenderTemplate(kStr, data, opts...)
			if err != nil {
				return nil, err
			}
			evaledVal, err := EvaluateValuesTemplates(item, data, opts...)
			if err != nil {
				return nil, err
			}
			res[evaledKey] = evaledVal
		}
		return res, nil
	case []any:
		res := make([]any, len(val))
		for i, item := range val {
			evaled, err := EvaluateValuesTemplates(item, data, opts...)
			if err != nil {
				return nil, err
			}
			res[i] = evaled
		}
		return res, nil
	default:
		return v, nil
	}
}

// splitEscaped splits s on unescaped occurrences of sep, keeping every backslash
// for the unescape pass that follows.
func splitEscaped(s string, sep byte) []string {
	var (
		parts   []string
		cur     strings.Builder
		escaped bool
	)
	for i := 0; i < len(s); i++ {
		switch {
		case escaped:
			escaped = false
			cur.WriteByte(s[i])
		case s[i] == '\\':
			escaped = true
			cur.WriteByte(s[i])
		case s[i] == sep:
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(s[i])
		}
	}
	return append(parts, cur.String())
}

// structuralChars are the characters this parser gives meaning to. A backslash
// before one of them removes that meaning, which is how .a.b\.c names the two
// keys "a" and "b.c".
//
// A backslash before anything else is left in place, both characters intact.
// Helm drops it, turning "C:\temp" into "C:temp"; keeping it is a deliberate
// divergence from upstream, because a Windows path is a plausible chart value
// and the backslash in one is not an escape attempt. Reconcile this with Zarf
// before the package is offered upstream.
const structuralChars = `.,[]{}=\`

// unescape resolves the escapes splitEscaped left in place.
func unescape(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var (
		b       strings.Builder
		escaped bool
	)
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case escaped:
			escaped = false
			if !strings.ContainsRune(structuralChars, r) {
				b.WriteRune('\\')
			}
			b.WriteRune(r)
		case r == '\\':
			escaped = true
		default:
			b.WriteRune(r)
		}
	}
	if escaped {
		// Trailing backslash with nothing to escape; keep it as typed.
		b.WriteRune('\\')
	}
	return b.String()
}

// typedScalar converts a raw string into the Go type Helm's strvals.typedVal
// would produce for it. It is what types the output of a rendered template, so
// that "{{ .Values.enabled }}" yields a real boolean. The rules are Helm's on
// purpose, including the two that look like omissions:
//
// A leading zero keeps the value a string. Image tags, zip codes and account
// numbers live in chart values, and turning "0755" into 755 corrupts them. Only
// a bare "0" is a number.
//
// Floats are not parsed at all, matching `helm --set`, where "1.5" is the string
// "1.5". Parsing them would silently turn a version string like "1.10" into 1.1
// and drop a digit. A values *file* still yields a real float for an unquoted
// 1.5, because YAML types it before this code sees it.
func typedScalar(val string) any {
	switch {
	case strings.EqualFold(val, "true"):
		return true
	case strings.EqualFold(val, "false"):
		return false
	case strings.EqualFold(val, "null"):
		return nil
	case val == "0":
		return int64(0)
	case val == "" || val[0] == '0':
		return val
	}
	if i, err := strconv.ParseInt(val, 10, 64); err == nil {
		return i
	}
	return val
}

// maxIndex bounds a list index in a value path, matching Helm's
// strvals.MaxIndex. A path arrives from a package definition or a config file,
// and the list is grown to fit the index, so without a bound ".a[9999999]" asks
// for a ten-million-element slice.
const maxIndex = 63

// pathSegment is one dot-separated step of a value path: a map key followed by
// any list indices written after it. "servers[0][1]" is a single segment whose
// key is "servers" and whose indices are 0 and 1.
type pathSegment struct {
	key     string
	indices []int
}

// valuePath converts a Zarf Values path into the segments SetValuePath and
// GetValuePath walk.
//
// The leading "." is required. ZEP-0021 writes every sourcePath and targetPath
// with one, and a lone "." names the root of the values. Requiring it also
// catches a path written in the dotted form a chart's own values use
// (foo.bar), which would otherwise silently address the wrong key.
func valuePath(path string) ([]pathSegment, error) {
	if !strings.HasPrefix(path, ".") {
		return nil, fmt.Errorf("invalid values path %q: must start with a dot", path)
	}
	if path == "." {
		// The root. No segments to walk.
		return nil, nil
	}
	return parsePath(path[1:])
}

// SetValuePath assigns val at a Zarf Values path within dst, creating the maps
// and lists along the way. The path must start with a dot; see valuePath.
//
// A path of "." addresses the root, so val must be a map there and is merged
// into dst rather than replacing it. That mirrors what a targetPath of "." means
// for a chart: contribute these keys to the chart's values, not overwrite all of
// them.
func SetValuePath(dst map[string]any, path string, val any) error {
	segs, err := valuePath(path)
	if err != nil {
		return err
	}
	if len(segs) == 0 {
		root, ok := normalizeValue(val).(map[string]any)
		if !ok {
			return fmt.Errorf("values path %q is the root, so the value must be a map, not %T", path, val)
		}
		mergeInto(dst, root)
		return nil
	}
	_, err = setSegments(dst, segs, val, "")
	return err
}

// ApplyMapping copies the value at source in values to target in dst, and
// reports whether values defined source at all.
//
// A source the values do not define leaves dst untouched, so whatever dst
// already held stands as the default. That is what makes a mapping safe to
// declare for every knob a package exposes: a cluster that says nothing about
// one of them gets the package's own setting, not a zero value.
func ApplyMapping(dst map[string]any, values map[string]any, source string, target string) (bool, error) {
	val, ok, err := GetValuePath(values, source)
	if err != nil {
		return false, fmt.Errorf("values source %q: %w", source, err)
	}
	if !ok {
		return false, nil
	}
	if err := SetValuePath(dst, target, val); err != nil {
		return false, fmt.Errorf("values target %q: %w", target, err)
	}
	return true, nil
}

// GetValuePath reads the value at a Zarf Values path in src. The path must start
// with a dot; see valuePath. A path of "." returns src itself.
//
// The second result reports whether the path is present. A path that runs into a
// scalar, a missing key, or past the end of a list is absent rather than an
// error, so a sourcePath a package leaves unset reads as missing instead of
// failing the whole mapping. Only a malformed path is an error.
func GetValuePath(src map[string]any, path string) (any, bool, error) {
	segs, err := valuePath(path)
	if err != nil {
		return nil, false, err
	}

	var cur any = src
	for _, seg := range segs {
		m, ok := asMap(cur)
		if !ok {
			return nil, false, nil
		}
		cur, ok = m[seg.key]
		if !ok {
			return nil, false, nil
		}
		for _, i := range seg.indices {
			list, ok := cur.([]any)
			if !ok || i >= len(list) {
				return nil, false, nil
			}
			cur = list[i]
		}
	}
	return cur, true, nil
}

// asMap reports whether v is a map this package can look a key up in, whatever
// concrete type produced it. Values reach here from a YAML decoder or from dig,
// so a plain type assertion would miss most of them.
func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case dig.Mapping:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, item := range m {
			out[fmt.Sprint(k)] = item
		}
		return out, true
	}
	return nil, false
}

// parsePath splits the dotted body of a value path into its segments.
func parsePath(path string) ([]pathSegment, error) {
	raws := splitEscaped(path, '.')
	segs := make([]pathSegment, 0, len(raws))
	for _, raw := range raws {
		seg, err := parseSegment(raw, path)
		if err != nil {
			return nil, err
		}
		segs = append(segs, seg)
	}
	return segs, nil
}

// parseSegment reads one segment of a path. full is carried only to name the
// whole path in an error.
func parseSegment(raw, full string) (pathSegment, error) {
	key, rest := splitKeyIndices(raw)
	seg := pathSegment{key: unescape(key)}
	if seg.key == "" {
		return seg, fmt.Errorf("invalid path %q: empty key", full)
	}
	for rest != "" {
		end := strings.IndexByte(rest, ']')
		if rest[0] != '[' || end == -1 {
			return seg, fmt.Errorf("invalid path %q: expected a [index] after %q", full, seg.key)
		}
		n, err := strconv.Atoi(rest[1:end])
		if err != nil {
			return seg, fmt.Errorf("invalid list index %q in path %q", rest[1:end], full)
		}
		if n < 0 {
			return seg, fmt.Errorf("negative list index %d in path %q", n, full)
		}
		if n > maxIndex {
			return seg, fmt.Errorf("list index %d in path %q exceeds the maximum of %d", n, full, maxIndex)
		}
		seg.indices = append(seg.indices, n)
		rest = rest[end+1:]
	}
	return seg, nil
}

// splitKeyIndices splits a segment at its first unescaped "[", so a key may
// contain a literal bracket when escaped.
func splitKeyIndices(raw string) (key, rest string) {
	escaped := false
	for i := 0; i < len(raw); i++ {
		switch {
		case escaped:
			escaped = false
		case raw[i] == '\\':
			escaped = true
		case raw[i] == '[':
			return raw[:i], raw[i:]
		}
	}
	return raw, ""
}

// setSegments assigns val at segs within cur and returns what cur becomes.
//
// The container is returned rather than mutated in place because a list element
// or a map that does not exist yet has to be created here and then stored back
// into its parent.
//
// at is the path already walked, used to name the offending key when a path runs
// into a value that is not the container it needs.
func setSegments(cur any, segs []pathSegment, val any, at string) (any, error) {
	if len(segs) == 0 {
		return val, nil
	}
	// asMap, rather than a plain assertion, so a write can walk a structure that
	// was decoded into dig.Mapping or map[any]any - the engine configuration is
	// one - and keep writing into the map that is already there.
	m, ok := asMap(cur)
	if !ok {
		if cur != nil {
			return nil, fmt.Errorf("path conflict at key %q: %T is not a map", at, cur)
		}
		m = make(map[string]any)
	}

	seg := segs[0]
	next := seg.key
	if at != "" {
		next = at + "." + seg.key
	}

	var (
		child any
		err   error
	)
	if len(seg.indices) > 0 {
		child, err = setIndices(m[seg.key], seg.indices, segs[1:], val, next)
	} else {
		child, err = setSegments(m[seg.key], segs[1:], val, next)
	}
	if err != nil {
		return nil, err
	}
	m[seg.key] = child
	return m, nil
}

// setIndices walks the list indices of one segment, growing each list to reach
// the index asked for. Skipped positions are left nil, which is what Helm does
// for "a[2]=x" on an empty list.
func setIndices(cur any, indices []int, rest []pathSegment, val any, at string) (any, error) {
	list, ok := cur.([]any)
	if !ok {
		if cur != nil {
			return nil, fmt.Errorf("path conflict at key %q: %T is not a list", at, cur)
		}
		list = []any{}
	}

	i := indices[0]
	for len(list) <= i {
		list = append(list, nil)
	}
	next := fmt.Sprintf("%s[%d]", at, i)

	var (
		child any
		err   error
	)
	if len(indices) > 1 {
		child, err = setIndices(list[i], indices[1:], rest, val, next)
	} else {
		child, err = setSegments(list[i], rest, val, next)
	}
	if err != nil {
		return nil, err
	}
	list[i] = child
	return list, nil
}

// MergeValues returns the recursive merge of src into dst, with src winning
// wherever both set a key.
//
// Neither argument is modified. Values files are merged repeatedly while
// composing a package, so an in-place merge would let one merge's result leak
// into the defaults that every later merge starts from.
//
// Nested maps are matched by shape rather than by Go type, so a dig.Mapping or a
// map[any]any from a YAML decoder deep-merges with a map[string]any instead of
// replacing that whole branch. The result is map[string]any all the way down.
func MergeValues(dst, src map[string]any) map[string]any {
	out := normalizeMap(dst)
	mergeInto(out, normalizeMap(src))
	return out
}

// mergeInto merges src into dst. Both are already normalized private copies, so
// mutating dst here cannot be observed by a caller.
func mergeInto(dst, src map[string]any) {
	for k, v := range src {
		if srcMap, ok := v.(map[string]any); ok {
			if dstMap, ok := dst[k].(map[string]any); ok {
				mergeInto(dstMap, srcMap)
				continue
			}
		}
		dst[k] = v
	}
}

// normalizeMap deep-copies m into a plain map[string]any. The constraint admits
// dig.Mapping as well, since it is a named map[string]any.
func normalizeMap[M ~map[string]any](m M) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = normalizeValue(v)
	}
	return out
}

// normalizeValue deep-copies v, converting every nested map to map[string]any.
// The copy is what makes MergeValues non-mutating; the conversion is what lets it
// recognise a nested map whatever concrete type produced it.
func normalizeValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return normalizeMap(val)
	case dig.Mapping:
		return normalizeMap(val)
	case map[any]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[fmt.Sprint(k)] = normalizeValue(item)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = normalizeValue(item)
		}
		return out
	default:
		return v
	}
}

// RenderValues renders any value (string, map, slice, scalar) into YAML values content.
func RenderValues(v any) (string, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}

	buf := bytes.Buffer{}
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}
