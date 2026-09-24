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

//go:build !windows

package ansiblemod

import (
	"fmt"
	"os"
	"syscall"
)

// newStdoutGuard takes over standard output. Call it as the first thing a module run does.
func newStdoutGuard() (*stdoutGuard, error) {
	fd, err := syscall.Dup(int(os.Stdout.Fd()))
	if err != nil {
		return nil, fmt.Errorf("unable to take over standard output: %w", err)
	}
	// The descriptor belongs to this process only. Nothing cargoship runs locally should inherit
	// a second handle on the channel the module answers Ansible over.
	syscall.CloseOnExec(fd)

	guard := &stdoutGuard{
		out:     os.NewFile(uintptr(fd), "ansible-module-stdout"),
		restore: os.Stdout,
	}
	os.Stdout = os.Stderr
	return guard, nil
}
