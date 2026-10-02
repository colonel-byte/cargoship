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
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// Invocation is one zarf command line to run.
type Invocation struct {
	// Binary is the zarf to run.
	Binary string
	// Args is the argument vector after the binary.
	Args []string
	// Dir is the working directory. Zarf init resolves the init package relative to it, so this is
	// load-bearing rather than cosmetic.
	Dir string
	// Env is added to the wrapper's own environment, as KEY=VALUE.
	Env []string
	// OnLine is called with each line zarf writes, if set. It is how progress is read back out of
	// the log stream.
	OnLine func(string)
}

// Result is what running zarf produced.
type Result struct {
	ExitCode int
}

// Runner runs a zarf command line. It is an interface so a test can prove the argument vector, the
// working directory, and the progress reading without a zarf binary on the machine.
type Runner interface {
	Run(ctx context.Context, in Invocation) (Result, error)
}

// ExecRunner runs zarf as a child process.
type ExecRunner struct{}

// Run executes the invocation and streams its output.
//
// Both of zarf's streams are wired to the wrapper's stderr, not inherited. Stdout belongs to the
// module result, and zarf writes progress rendering to its own stdout: letting the child inherit
// fd 1 would put that rendering in the middle of the JSON object Ansible parses. Ansible shows
// stderr as module output, so nothing is lost by moving it there.
func (ExecRunner) Run(ctx context.Context, in Invocation) (Result, error) {
	if in.Binary == "" {
		return Result{}, errors.New("no zarf binary to run")
	}

	cmd := exec.CommandContext(ctx, in.Binary, in.Args...) //nolint:gosec // the operator named the binary and the parameters
	cmd.Dir = in.Dir
	cmd.Env = append(os.Environ(), in.Env...)
	// Zarf reads nothing from stdin: the module renders --confirm, because there is no terminal on
	// the other end of a playbook task to confirm at. Closing it means a zarf that asks anyway
	// fails rather than hangs until the task's timeout.
	cmd.Stdin = nil

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("unable to read zarf's output: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, fmt.Errorf("unable to read zarf's output: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("unable to run %s: %w", in.Binary, err)
	}

	var wg sync.WaitGroup
	// Both streams feed one line handler, so the handler is called from one goroutine at a time.
	var lines sync.Mutex
	for _, stream := range []io.Reader{stdout, stderr} {
		wg.Add(1)
		go func(r io.Reader) {
			defer wg.Done()
			scanner := bufio.NewScanner(r)
			// Zarf's JSON log records carry resource manifests and error bodies, which outrun the
			// scanner's default 64KiB line.
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				line := scanner.Text()
				fmt.Fprintln(os.Stderr, line)
				if in.OnLine == nil {
					continue
				}
				lines.Lock()
				in.OnLine(line)
				lines.Unlock()
			}
		}(stream)
	}
	wg.Wait()

	waitErr := cmd.Wait()
	res := Result{ExitCode: cmd.ProcessState.ExitCode()}
	if waitErr != nil {
		return res, waitErr
	}
	return res, nil
}
