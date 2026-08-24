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

package config

// These are the CPU architectures Zarf supports building/targeting for.
const (
	// OSArchAMD64 is the x86-64, 64-bit AMD, architecture.
	OSArchAMD64 = "amd64"
	// OSArchARM64 is the 64-bit ARM architecture.
	OSArchARM64 = "arm64"
	// OSArchRISCV is the 64-bit RISC-V architecture.
	OSArchRISCV = "riscv64"
)
