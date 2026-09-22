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

// Package heartbeat writes live phase execution progress to a status file on disk so long-running
// operations (such as Ansible modules) can be monitored externally in real-time.
package heartbeat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// EnvStatusFile defines an optional environment variable specifying the path to write heartbeat status.
const EnvStatusFile = "CARGOSHIP_STATUS_FILE"

// Status holds the real-time progress of a phase run.
type Status struct {
	Phase     string    `json:"phase"`
	Index     int       `json:"index"`
	Total     int       `json:"total"`
	Status    string    `json:"status"` // "running", "completed", "failed"
	Timestamp time.Time `json:"timestamp"`
	Error     string    `json:"error,omitempty"`
}

var (
	mu         sync.Mutex
	statusPath string
)

// Init initializes the heartbeat file target.
func Init(path string) {
	mu.Lock()
	defer mu.Unlock()
	if path == "" {
		path = os.Getenv(EnvStatusFile)
	}
	statusPath = path
}

// Update writes the current phase progress to the status file if configured.
func Update(phase string, index, total int, state string, err error) {
	mu.Lock()
	defer mu.Unlock()

	target := statusPath
	if target == "" {
		target = os.Getenv(EnvStatusFile)
	}
	if target == "" {
		return
	}

	st := Status{
		Phase:     phase,
		Index:     index,
		Total:     total,
		Status:    state,
		Timestamp: time.Now().UTC(),
	}
	if err != nil {
		st.Error = err.Error()
	}

	data, marshalErr := json.MarshalIndent(st, "", "  ")
	if marshalErr != nil {
		return
	}

	dir := filepath.Dir(target)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755) //nolint:errcheck
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", target, time.Now().UnixNano())
	if writeErr := os.WriteFile(tmpFile, data, 0o644); writeErr == nil {
		_ = os.Rename(tmpFile, target) //nolint:errcheck
	}
}

// Clear removes the status file after successful execution.
func Clear() {
	mu.Lock()
	defer mu.Unlock()
	if statusPath != "" {
		_ = os.Remove(statusPath) //nolint:errcheck
	}
}
