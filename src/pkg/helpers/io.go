// Copyright 2024 Defense Unicorns authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from pkg:
// https://github.com/defenseunicorns/pkg
//
// Modifications Copyright 2026 colonel-byte.
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

package helpers

const (
	// ReadUser is used for any internal file to be read only
	ReadUser = 0400
	// ReadWriteUser is used for any internal file not normally used by the end user or containing sensitive data
	ReadWriteUser = 0600
	// ReadAllWriteUser is used for any non sensitive file intended to be consumed by the end user
	ReadAllWriteUser = 0644
	// ReadWriteExecuteUser is used for any directory or executable not normally used by the end user or containing sensitive data
	ReadWriteExecuteUser = 0700
	// ReadExecuteAllWriteUser is used for any non sensitive directory or executable intended to be consumed by the end user
	ReadExecuteAllWriteUser = 0755
)
