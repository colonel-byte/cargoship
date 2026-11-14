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

package distro

import "fmt"

// A package's name and version both end up inside OCI references, and those have hard limits an
// operator never sees until a push fails. `cargoship publish` lands a package at
// <registry>/<name>:<version>, so the name is a repository path component and the version is a
// tag; a mirror writes a record whose reference is built from both.
//
// The limits below leave room inside the two the registry specification sets:
//
//   - a tag is at most 128 characters: `[\w][\w.-]{0,127}`
//   - a repository name is at most 255 characters in total, host excluded
//
// What makes a bound necessary rather than tidy is that neither limit is reliably enforced on the
// way out. oras's tag pattern does catch an over-long tag, but its repository pattern does not
// implement the 255-character total -- a 300-character name parses locally and is refused by the
// registry, after the package has been built and carried. Bounding the name here is what moves
// that failure to the moment the definition is read.
//
// The numbers are deliberately generous. The longest name in this repository is 18 characters
// ("rancher-k3s-multi"), so 64 is roughly three times anything real, and the pair still fits a
// record reference with 7 characters to spare.
const (
	// MaxNameLength bounds Metadata.Name, which becomes a repository path component.
	MaxNameLength = 64
	// MaxVersionLength bounds Metadata.Version, which becomes a tag.
	MaxVersionLength = 48
)

// ValidateReferenceLengths reports whether a package's name and version fit the references built
// from them.
//
// It is a runtime check as well as a schema constraint because the two catch different things.
// The schema's maxLength is what an editor and `cargoship validate` enforce, and it is the right
// place for the rule to be published; but the load path does not apply the schema, so without
// this a `cargoship create` would build the package anyway. It is also the only check that can
// speak for a package built by an older cargoship, where the definition is no longer available
// and the only thing left is the metadata inside the archive.
//
// An empty version is not an error here. Whether a version is required is the caller's rule --
// publishing demands one, mirroring tolerates a package without one -- and this function answers
// only the question it is named for.
func ValidateReferenceLengths(name, version string) error {
	if len(name) > MaxNameLength {
		return fmt.Errorf("the package name is %d characters, over the %d allowed: it becomes a repository path in every reference built from this package, and a registry refuses a longer one", len(name), MaxNameLength)
	}
	if len(version) > MaxVersionLength {
		return fmt.Errorf("the package version is %d characters, over the %d allowed: it becomes the tag in every reference built from this package, and a registry refuses a longer one", len(version), MaxVersionLength)
	}
	return nil
}
