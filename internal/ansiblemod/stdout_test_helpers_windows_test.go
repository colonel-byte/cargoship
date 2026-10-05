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

//go:build windows

package ansiblemod

import (
	"os"
	"syscall"
)

// setStdHandle is missing from the standard syscall package (only GetStdHandle is wrapped), so
// it is called directly from kernel32.
var procSetStdHandle = syscall.NewLazyDLL("kernel32.dll").NewProc("SetStdHandle")

// dupStdout and redirectStdout move the process's STD_OUTPUT_HANDLE itself rather than the
// os.Stdout variable -- the Windows equivalent of the unix helpers moving file descriptor 1.
//
// The guard under test duplicates the handle, so a test that only reassigned the variable would
// be handed the test binary's own standard output and would prove nothing.
func dupStdout() (*os.File, error) {
	current, err := syscall.GetCurrentProcess()
	if err != nil {
		return nil, err
	}

	var dup syscall.Handle
	src := syscall.Handle(os.Stdout.Fd())
	if err := syscall.DuplicateHandle(current, src, current, &dup, 0, false, syscall.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(dup), "saved-stdout"), nil
}

func redirectStdout(to *os.File) error {
	stdOutputHandle := int32(syscall.STD_OUTPUT_HANDLE)
	r1, _, err := procSetStdHandle.Call(uintptr(uint32(stdOutputHandle)), to.Fd())
	if r1 == 0 {
		return err
	}
	return nil
}
