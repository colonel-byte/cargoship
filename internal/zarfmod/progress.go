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
	"regexp"
	"strings"
)

// progress turns zarf's log stream into heartbeats and a list of what ran.
//
// This is the part of the wrapper that a module inside zarf itself would not need. ZEP-0072 has
// zarf emit heartbeats from the deployer, where the component list and the phase boundaries are
// already known. A wrapper has no such access: what it has is zarf's log output, so it reads the
// component boundaries back out of it. That is the cost of proving the shape from outside, and it
// is the piece to delete first if dispatch ever moves into zarf.
type progress struct {
	hb *Heartbeat
	// total is how many components the run is expected to deploy, 0 when the wrapper cannot know.
	// Zarf is told which components to deploy only when the operator named them, and the init
	// package's own component list is inside the package.
	total int

	index      int
	current    string
	components []string
}

// componentStart matches the console rendering of a component boundary, for a run whose log format
// the operator overrode. The JSON path below is the one the module asks for.
var componentStart = regexp.MustCompile(`(?i)deploying(?: zarf)? component[ :-]+"?([a-zA-Z0-9._-]+)"?`)

// componentKeys are the slog attribute names zarf has used to carry a component's name. More than
// one is accepted because the wrapper reads a log stream rather than an API, and a renamed
// attribute should cost the progress display rather than the run.
var componentKeys = []string{"name", "component", "componentName"}

func newProgress(hb *Heartbeat, total int) *progress {
	return &progress{hb: hb, total: total}
}

// line reads one line of zarf output and reports a heartbeat when it names a new component.
func (p *progress) line(s string) {
	name, ok := componentName(s)
	if !ok || name == p.current {
		return
	}

	// Zarf deploys components one at a time, so the start of one is the end of the last. Nothing
	// in the log says a component succeeded, only that the next began, which is why the heartbeat
	// for the finished one reports completed without claiming anything about what it changed.
	if p.current != "" {
		p.hb.Update(p.current, p.index, p.total, "completed", nil)
	}

	p.index++
	p.current = name
	p.components = append(p.components, name)
	p.hb.Update(name, p.index, p.total, "running", nil)
}

// finish reports the last component as finished. state is completed for a run that succeeded and
// failed for one that did not, and cause is reported with it.
func (p *progress) finish(state string, cause error) {
	if p.current == "" {
		return
	}
	p.hb.Update(p.current, p.index, p.total, state, cause)
	p.current = ""
}

// componentName returns the component a log line announces, and whether it announced one.
func componentName(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false
	}

	if strings.HasPrefix(line, "{") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err == nil {
			msg, isText := record["msg"].(string)
			if !isText || !announcesComponent(msg) {
				return "", false
			}
			for _, key := range componentKeys {
				if name, ok := record[key].(string); ok && name != "" {
					return name, true
				}
			}
			// The line said a component was starting and did not say which. The message is
			// better than nothing: a display reading "Deploying component" still tells an
			// operator the run moved.
			return strings.TrimSpace(msg), true
		}
	}

	if m := componentStart.FindStringSubmatch(line); m != nil {
		return m[1], true
	}
	return "", false
}

func announcesComponent(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "component") && strings.Contains(msg, "deploy")
}
