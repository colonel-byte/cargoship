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
	"encoding/json"
	"fmt"
	"io"
)

// Changed signal values reported in Detail.ChangedSignal.
const (
	// SignalUnknown means no phase reported whether it changed anything, so the changed field
	// above it is a convention rather than an observation.
	SignalUnknown = "unknown"
	// SignalPartial means some phases reported and some did not. The ones that did not are
	// named in Detail.ChangedUndeclared.
	SignalPartial = "partial"
	// SignalComplete means every phase the run reached reported whether it changed anything.
	SignalComplete = "complete"
)

// Response is the single JSON object a module writes.
//
// The field names outside the cargoship key are Ansible's, not ours: changed, failed, and msg are
// what every module returns and what a playbook's conditionals are written against. Everything
// cargoship has to say beyond those is nested under one key, so it cannot collide with a field
// Ansible gives a meaning to later.
type Response struct {
	Changed   bool    `json:"changed"`
	Failed    bool    `json:"failed,omitempty"`
	Msg       string  `json:"msg,omitempty"`
	Cargoship *Detail `json:"cargoship,omitempty"`
}

// Detail is cargoship's own report on the run.
type Detail struct {
	// Module is the action that ran, so a failure in a playbook that loops over several says
	// which one.
	Module string `json:"module,omitempty"`
	// InventoryPath is the generated ZarfCluster document. It is the file cargoship was
	// actually given, which is the thing to look at when a translation is wrong.
	InventoryPath string `json:"inventoryPath,omitempty"`
	// InventoryKept says whether that file still exists. A run that fails keeps it; a run that
	// succeeds removes it unless the operator named the path themselves.
	InventoryKept bool `json:"inventoryKept,omitempty"`
	// CheckMode says whether this was a dry run, so a playbook cannot mistake a check-mode
	// result for a real one.
	CheckMode bool `json:"checkMode,omitempty"`
	// Command is the cargoship command line the module ran, for an operator reproducing it by
	// hand. Parameters carrying secrets are passed as file paths rather than values, so this
	// holds no key material.
	Command []string `json:"command,omitempty"`
	// PhasesRan and PhasesPlanned name the phases the run executed and, under check mode, the
	// phases it reported instead of running. Ansible shows one result for the whole fleet, so
	// these are what a playbook has in place of per-host detail.
	PhasesRan     []string `json:"phasesRan,omitempty"`
	PhasesPlanned []string `json:"phasesPlanned,omitempty"`
	// ChangedSignal says how much the changed field above is worth: complete when every phase
	// the run reached said whether it changed anything, partial when some did not, unknown when
	// no phase reported at all.
	ChangedSignal string `json:"changedSignal,omitempty"`
	// ChangedUndeclared names the phases that did not say. It is what makes a partial signal
	// actionable rather than a warning with nothing behind it.
	ChangedUndeclared []string `json:"changedUndeclared,omitempty"`
}

// fail marks the response as a failure carrying err.
//
// changed is left alone. A failure part-way through an apply has usually already changed hosts,
// and reporting otherwise would tell a playbook to skip the handlers that clean up after it.
func (r *Response) fail(err error) {
	r.Failed = true
	r.Msg = err.Error()
}

// emit writes the response as exactly one JSON object.
func (r *Response) emit(w io.Writer) error {
	b, err := json.Marshal(r)
	if err != nil {
		// Nothing in Response is unmarshalable, so reaching here means a field was added that
		// is. Answer with something Ansible can parse rather than with nothing at all.
		b = []byte(fmt.Sprintf(`{"failed":true,"changed":false,"msg":%q}`,
			"unable to encode the module result: "+err.Error()))
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("unable to write the module result: %w", err)
	}
	return nil
}
