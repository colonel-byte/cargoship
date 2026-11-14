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

package phase

import (
	"context"
	"sync"

	"github.com/colonel-byte/cargoship/internal/heartbeat"
)

// State is what a phase is doing, as something watching a run sees it.
//
// The values are the strings the heartbeat file has always carried, because they are parsed
// outside this repository: the Ansible action plugin reads `status` out of that JSON to print its
// progress line. Declaring them here rather than passing literals is what keeps the file and the
// sink below from drifting into two vocabularies for one thing.
type State string

// The states a phase passes through. Every phase in the plan reports running and then one of the
// other two, including a phase whose ShouldRun is false -- the position counts the plan, not the
// phases that acted.
const (
	// StateRunning is a phase that has been entered.
	StateRunning State = "running"
	// StateCompleted is a phase that finished without an error.
	StateCompleted State = "completed"
	// StateFailed is a phase that ended the run.
	StateFailed State = "failed"
)

// Progress is where a run has got to: one phase, its position in the plan, and what it is doing.
type Progress struct {
	// Phase is the phase's Title, which is what an operator recognises -- "Upload files to
	// hosts", "Upgrade Controller".
	Phase string
	// Index is the phase's position in the plan, from 1, and Total is how many it holds. The
	// plan is every phase the action registered, so the pair is stable for the whole run.
	Index int
	Total int
	State State
	// Err is the failure that ended the run, set only with StateFailed.
	Err error
}

// progressSinkKey is the context key a sink is carried under.
type progressSinkKey struct{}

// ProgressSink carries the phase a run is on to a caller watching from another goroutine.
//
// It exists because an apply is one blocking call that takes minutes, and a caller that is not
// the one driving it has no way to ask where it is. The heartbeat file answers that for the
// Ansible modules and answers it badly for anything else: the path is process-wide state, so two
// runs in one process would write the same file, and a reader has to poll a filesystem to learn
// something the process already knows.
//
// Scoped to a context rather than global for exactly that reason -- `cluster-api-provider-cargoship`
// holds one in-flight apply per custom resource in one process, and each needs its own answer.
// The file is still written whatever is attached here; the two watchers are independent.
//
// A sink holds the latest Progress and nothing else. A caller wanting the whole sequence should
// read it as often as it cares about, which is what a controller writing a status field does
// anyway; ResultSink is the record of what a finished run did.
type ProgressSink struct {
	mu      sync.Mutex
	current Progress
}

// WithProgressSink returns a context carrying a sink, and the sink itself.
func WithProgressSink(ctx context.Context) (context.Context, *ProgressSink) {
	s := &ProgressSink{}
	return context.WithValue(ctx, progressSinkKey{}, s), s
}

// Current is the phase the run is on, or the zero Progress before it enters its first one.
func (s *ProgressSink) Current() Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func (s *ProgressSink) update(p Progress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = p
}

// reportProgress tells everything watching a run where it has got to: the heartbeat file the
// Ansible modules poll, and the sink a caller attached to the context.
//
// One call site for both, because two of them in the loop is how a state comes to reach one
// watcher and not the other.
func reportProgress(ctx context.Context, phase string, index, total int, state State, err error) {
	heartbeat.Update(phase, index, total, string(state), err)

	if sink, ok := ctx.Value(progressSinkKey{}).(*ProgressSink); ok {
		sink.update(Progress{
			Phase: phase,
			Index: index,
			Total: total,
			State: state,
			Err:   err,
		})
	}
}
