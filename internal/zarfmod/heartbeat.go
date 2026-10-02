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
	"os"
	"path/filepath"
	"sync"
	"time"
)

// EnvStatusFile names the file live progress is written to. The action plugin creates the file and
// exports this variable; nothing is written when it is unset, which is what makes a run by hand
// behave the same as a run under Ansible minus the display.
//
// internal/heartbeat already does this for cargoship, with CARGOSHIP_STATUS_FILE and a
// package-global target. It is not reused here: the variable name is part of the zarf contract
// ZEP-0072 describes, and a process that runs one module per invocation has no reason to keep the
// target in a global.
const EnvStatusFile = "ZARF_STATUS_FILE"

// Status is one heartbeat. The shape is the one ZEP-0072 specifies, so the monitor written against
// the proposal reads what this writes.
type Status struct {
	Phase     string    `json:"phase"`
	Index     int       `json:"index"`
	Total     int       `json:"total"`
	Status    string    `json:"status"` // running, completed, failed
	Timestamp time.Time `json:"timestamp"`
	Error     string    `json:"error,omitempty"`
}

// Heartbeat writes progress to a status file for an external monitor to read.
//
// Every write is a whole file written beside the target and renamed into place, because the reader
// is a Python thread polling on its own clock and a reader that can see half a document is a
// reader that has to be taught to ignore one.
type Heartbeat struct {
	mu   sync.Mutex
	path string
}

// NewHeartbeat returns a heartbeat writing to path, or to the file named by ZARF_STATUS_FILE when
// path is empty. A heartbeat with no target still accepts every call and writes nothing.
func NewHeartbeat(path string) *Heartbeat {
	if path == "" {
		path = os.Getenv(EnvStatusFile)
	}
	return &Heartbeat{path: path}
}

// Path is the file being written, empty when there is none.
func (h *Heartbeat) Path() string {
	if h == nil {
		return ""
	}
	return h.path
}

// Update writes one heartbeat.
//
// Failures are dropped on purpose. This is a side channel for a progress display: a run that is
// deploying a cluster must not fail because the directory holding its status file went away.
func (h *Heartbeat) Update(phase string, index, total int, state string, cause error) {
	if h == nil || h.path == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	st := Status{
		Phase:     phase,
		Index:     index,
		Total:     total,
		Status:    state,
		Timestamp: time.Now().UTC(),
	}
	if cause != nil {
		st.Error = cause.Error()
	}

	data, err := json.Marshal(st)
	if err != nil {
		return
	}

	tmp := fmt.Sprintf("%s.tmp.%d", h.path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, h.path); err != nil {
		_ = os.Remove(tmp) //nolint:errcheck // a side channel cannot fail the run
	}
}

// Clear removes the status file. The action plugin removes it too, in a finally block; this is for
// the invocation that was not driven by one.
func (h *Heartbeat) Clear() {
	if h == nil || h.path == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	_ = os.Remove(h.path) //nolint:errcheck // best effort
	if matches, err := filepath.Glob(h.path + ".tmp.*"); err == nil {
		for _, m := range matches {
			_ = os.Remove(m) //nolint:errcheck // best effort
		}
	}
}
