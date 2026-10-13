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

package v1alpha1

// Kind identifies the kind of a colonel-byte.dev v1alpha1 document, for example Cluster or Distro.
type Kind string

const (
	// APIVersion is the API version string for the colonel-byte.dev v1alpha1 package.
	APIVersion string = "colonel-byte.dev/v1alpha1"
	// DeprecatedAPIVersion is the API group these documents used before the move to
	// colonel-byte.dev. It is still accepted so that documents written against an older
	// cargoship keep parsing, and it names an API group this project does not own: upstream
	// Zarf defines its own kinds under colonel-byte.dev/v1alpha1, which is why the move happened.
	DeprecatedAPIVersion string = "zarf.dev/v1alpha1"
)
