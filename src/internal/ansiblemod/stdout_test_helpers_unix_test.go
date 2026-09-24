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
	"os"
	"syscall"
)

// dupStdout and redirectStdout move file descriptor 1 itself rather than the os.Stdout variable.
//
// The guard under test duplicates the descriptor, so a test that only reassigned the variable
// would be handed the test binary's own standard output and would prove nothing.
func dupStdout() (*os.File, error) {
	fd, err := syscall.Dup(int(os.Stdout.Fd()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "saved-stdout"), nil
}

func redirectStdout(to *os.File) error {
	return syscall.Dup2(int(to.Fd()), int(os.Stdout.Fd()))
}
