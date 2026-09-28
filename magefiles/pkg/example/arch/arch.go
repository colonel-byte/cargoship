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

// Package arch is the architecture coverage the generated examples share.
//
// It is its own package because both magefiles/pkg/example and magefiles/pkg/example/upstream
// render multi-architecture examples and have to agree on which architectures those are and how
// each is spelled upstream.
package arch

// Multi is the architecture set a multi-architecture example covers.
//
// The multi-architecture flavors exist to show what a package covering more than one
// architecture looks like, not to cover every architecture upstream builds: two is enough to
// read, and it keeps the arm64 artifacts the shasum cache has to hold down to a handful.
var Multi = []string{"amd64", "arm64"}

// RPM maps a Go architecture to the name an RPM repository publishes it under.
var RPM = map[string]string{
	"amd64": "x86_64",
	"arm64": "aarch64",
}
