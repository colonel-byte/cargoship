// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
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

package lang

const (
	// CmdDistroSignExample sign example
	CmdDistroSignExample = `
# Sign an unsigned package
$ cargoship sign cargoship-rancher-rke2-amd64-1.0.0.tar.zst --signing-key ./private-key.pem

# Re-sign with a new key (overwrite existing signature)
$ cargoship sign cargoship-rancher-rke2-amd64-1.0.0.tar.zst --signing-key ./new-key.pem --overwrite

# Sign a package from an OCI registry and output to a local directory
$ cargoship sign oci://ghcr.io/my-org/my-package:1.0.0 --signing-key ./private-key.pem --output ./signed/

# Sign a package and publish directly to an OCI registry
$ cargoship sign cargoship-rancher-rke2-amd64-1.0.0.tar.zst --signing-key ./private-key.pem --output oci://ghcr.io/my-org/signed-packages

# Sign with a cloud KMS key
$ cargoship sign cargoship-rancher-rke2-amd64-1.0.0.tar.zst --signing-key awskms://alias/my-signing-key
`
)
