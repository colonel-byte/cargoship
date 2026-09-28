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

// Package example renders the distro.yaml examples under example/ from the shared templates in
// magefiles/templates.
//
// Everything about an example follows from one upstream tag and the flavor being rendered,
// except the image lists and digests, which come from that release's own published assets. What
// differs between distros is pushed into the hooks on distroSpec rather than into a copy of the
// rendering per distro; upstream (kubeadm) is different enough to need its own package, in
// example/upstream.
package example
