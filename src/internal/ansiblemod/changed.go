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
	"github.com/colonel-byte/cargoship/src/pkg/phase"
)

// reportChanged fills in the response's changed field and cargoship.changedSignal from what the
// phases said about themselves.
//
// Under check mode a phase reports nothing, because it did nothing. What a check-mode run has
// instead is the list of phases it would have run, so changed there means "a real run would
// change something", which is what Ansible's check mode means by it.
//
// A phase that did not say anything cannot make changed true, so the signal is reported as
// partial and the silent phases are named. That is the honest answer: telling a playbook
// changed: false while several phases never said is not a claim cargoship can make, and saying
// changed: true regardless is what made the signal worthless before any phase reported.
func reportChanged(resp *Response, sink *phase.ResultSink, checkMode bool) {
	if !sink.Observed() {
		// The command returned success without any phase pipeline running. Nothing is known,
		// so fall back to the convention: an Ansible handler that fires when nothing happened
		// is a smaller failure than one that stays silent when something did.
		resp.Changed = true
		resp.Cargoship.ChangedSignal = SignalUnknown
		return
	}

	result := sink.Result()
	resp.Cargoship.PhasesRan = phase.Titles(result.Ran)
	resp.Cargoship.PhasesPlanned = phase.Titles(result.Planned)

	if checkMode {
		resp.Changed = result.Outstanding()
	} else {
		resp.Changed = result.Changed()
	}

	if result.Complete() {
		resp.Cargoship.ChangedSignal = SignalComplete
		return
	}
	resp.Cargoship.ChangedSignal = SignalPartial
	resp.Cargoship.ChangedUndeclared = result.Undeclared()
}
