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

package zarfmod

import (
	"encoding/json"
	"fmt"
	"io"
)

// Changed signal values reported in Detail.ChangedSignal.
const (
	// SignalUnknown means nothing the run reported says whether it changed anything, so the
	// changed field above it is a convention rather than an observation.
	SignalUnknown = "unknown"
	// SignalPartial means some of the run reported and some did not. For init that is the normal
	// answer: zarf says which components it deployed, and says nothing about whether a component
	// found the cluster already in the state it wanted.
	SignalPartial = "partial"
	// SignalComplete means every component the run reached reported whether it changed anything.
	// Nothing produces this yet; it is here because the field is the proposal's contract and a
	// value the wrapper can never emit is a thing a reviewer should see rather than guess at.
	SignalComplete = "complete"
)

// Response is the single JSON object a module writes.
//
// The field names outside the zarf key are Ansible's, not ours: changed, failed, and msg are what
// every module returns and what a playbook's conditionals are written against. Everything the
// wrapper has to say beyond those is nested under one key, so it cannot collide with a field
// Ansible gives a meaning to later.
type Response struct {
	Changed bool `json:"changed"`
	Failed  bool `json:"failed,omitempty"`
	// Skipped is how a module says it did not run at all. It is set when an operator asked for
	// check mode, because zarf init has no dry run to answer with.
	Skipped bool    `json:"skipped,omitempty"`
	Msg     string  `json:"msg,omitempty"`
	Zarf    *Detail `json:"zarf,omitempty"`
}

// Detail is the wrapper's own report on the run.
type Detail struct {
	// Module is the action that ran, so a failure in a playbook that loops over several says which
	// one.
	Module string `json:"module,omitempty"`
	// CheckMode says whether this was a dry run, so a playbook cannot mistake a check-mode result
	// for a real one.
	CheckMode bool `json:"checkMode,omitempty"`
	// Command is the zarf command line the wrapper ran, for an operator reproducing it by hand.
	// Credential flag values are replaced before this is reported; see redact.
	Command []string `json:"command,omitempty"`
	// Directory is where that command ran. Zarf init finds the init package relative to the
	// working directory, so the directory is part of what the command means.
	Directory string `json:"directory,omitempty"`
	// ExitCode is zarf's exit status. It is reported for a failure, where the message alone does
	// not distinguish a zarf that refused from a zarf that was killed.
	ExitCode int `json:"exitCode,omitempty"`
	// ComponentsRan names the init components zarf reported deploying, in the order it reported
	// them. Ansible shows one result for the whole run, so this is what a playbook has in place of
	// per-component tasks.
	ComponentsRan []string `json:"componentsRan,omitempty"`
	// ChangedSignal says how much the changed field above is worth.
	ChangedSignal string `json:"changedSignal,omitempty"`
	// ChangedUndeclared names what did not say. It is what makes a partial signal actionable
	// rather than a warning with nothing behind it.
	ChangedUndeclared []string `json:"changedUndeclared,omitempty"`
	// StatusFile is the heartbeat file the run wrote progress to, when one was asked for. It is
	// reported so a failed run leaves a trail even if the monitor that was watching it is gone.
	StatusFile string `json:"statusFile,omitempty"`
}

// fail marks the response as a failure carrying err.
//
// changed is left alone. A zarf init that fails part-way through has usually already deployed
// components, and reporting otherwise would tell a playbook to skip the handlers that clean up
// after it.
func (r *Response) fail(err error) {
	r.Failed = true
	r.Msg = err.Error()
}

// emit writes the response as exactly one JSON object.
func (r *Response) emit(w io.Writer) error {
	b, err := json.Marshal(r)
	if err != nil {
		// Nothing in Response is unmarshalable, so reaching here means a field was added that is.
		// Answer with something Ansible can parse rather than with nothing at all.
		b = []byte(fmt.Sprintf(`{"failed":true,"changed":false,"msg":%q}`,
			"unable to encode the module result: "+err.Error()))
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("unable to write the module result: %w", err)
	}
	return nil
}
