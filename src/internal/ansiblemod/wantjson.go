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

package ansiblemod

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
)

// controlPrefix marks the keys Ansible injects into every module's arguments. They are not the
// module's parameters and an unrecognised one is not an operator's mistake, so they are read for
// what cargoship understands and otherwise ignored.
const controlPrefix = "_ansible_"

// Control is the part of a module's arguments Ansible writes rather than the operator.
type Control struct {
	CheckMode bool `json:"_ansible_check_mode"`
	Diff      bool `json:"_ansible_diff"`
	NoLog     bool `json:"_ansible_no_log"`
	Verbosity int  `json:"_ansible_verbosity"`
}

// Args is the arguments file Ansible writes for a WANT_JSON module.
//
// The parameters are held undecoded so that unknown ones can be reported by name. A binary module
// has no argument_spec, so this is the whole of what stands between a mistyped parameter and a run
// that silently ignores it, and the ADR accepts the hand-written check on the condition that it
// names what was wrong.
type Args struct {
	Control Control
	params  map[string]json.RawMessage
}

// ReadArgs reads and parses the arguments file at path.
func ReadArgs(path string) (*Args, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the path is the one Ansible passed as argv[1]
	if err != nil {
		return nil, fmt.Errorf("unable to read the module arguments file: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("unable to parse the module arguments file %s: %w", path, err)
	}

	args := &Args{params: make(map[string]json.RawMessage, len(raw))}
	control := make(map[string]json.RawMessage)
	for name, value := range raw {
		if strings.HasPrefix(name, controlPrefix) {
			control[name] = value
			continue
		}
		args.params[name] = value
	}

	// Decoded leniently on purpose: Ansible adds control keys between releases, and a module that
	// refuses to run because it met a new one is worse than a module that ignores it.
	encoded, err := json.Marshal(control)
	if err != nil {
		return nil, fmt.Errorf("unable to read the Ansible control arguments: %w", err)
	}
	if err := json.Unmarshal(encoded, &args.Control); err != nil {
		return nil, fmt.Errorf("unable to read the Ansible control arguments: %w", err)
	}

	return args, nil
}

// Params decodes the module's own parameters into out, which must be a pointer to a struct whose
// JSON tags name them.
//
// A parameter the struct does not name is an error naming it, and the error lists what the module
// does accept. Nested documents are held to the same rule by the decoder, so a misspelled key
// inside the inventory argument fails here rather than translating into a cluster nobody asked for.
func (a *Args) Params(out any) error {
	accepted := jsonFieldNames(reflect.TypeOf(out))

	var unknown []string
	for name := range a.params {
		if !accepted[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown parameter %s; this module accepts %s",
			quoteAll(unknown), quoteAll(sortedKeys(accepted)))
	}

	encoded, err := json.Marshal(a.params)
	if err != nil {
		return fmt.Errorf("unable to read the module parameters: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("unable to read the module parameters: %w", err)
	}
	return nil
}

// jsonFieldNames returns the JSON names of a struct type's exported fields, following pointers and
// embedded structs so that a parameter set assembled from more than one struct is still listed in
// full.
func jsonFieldNames(t reflect.Type) map[string]bool {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	names := map[string]bool{}
	if t == nil || t.Kind() != reflect.Struct {
		return names
	}
	for i := range t.NumField() {
		field := t.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		// An embedded struct's fields are promoted, and encoding/json promotes them whether or
		// not the embedded type itself is exported, so the exportedness check below cannot come
		// first.
		if field.Anonymous && name == "" {
			for embedded := range jsonFieldNames(field.Type) {
				names[embedded] = true
			}
			continue
		}
		if !field.IsExported() {
			continue
		}
		if name == "" {
			name = field.Name
		}
		names[name] = true
	}
	return names
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func quoteAll(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, fmt.Sprintf("%q", n))
	}
	return strings.Join(quoted, ", ")
}
