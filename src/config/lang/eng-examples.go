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
	// CmdDistroApplyExample apply example
	CmdDistroApplyExample = `# Bootstrap or upgrade a cluster from a package and a config file
$ cargoship apply ./build/cargoship-distro-amd64.tar.zst --config ./cargoship-config.yaml --confirm

# Decrypt vault-encrypted registry credentials with a password file
$ cargoship apply ./build/cargoship-distro-amd64.tar.zst --config ./cargoship-config.yaml --confirm --vault-password-file ./vault-pass.txt

# Upgrade workers 25% at a time instead of all at once
$ cargoship apply ./build/cargoship-distro-amd64.tar.zst --config ./cargoship-config.yaml --confirm --work-concurrency 25%

# Update /etc/hosts, the firewall, and fapolicyd on every node as part of the apply
$ cargoship apply ./build/cargoship-distro-amd64.tar.zst --config ./cargoship-config.yaml --confirm -H -F -f

# Add the node-role label to each node, and leave the local kubeconfig untouched
$ cargoship apply ./build/cargoship-distro-amd64.tar.zst --config ./cargoship-config.yaml --confirm --label-nodes --update-kubeconfig=false`

	// CmdDistroPrepareExample prepare example
	CmdDistroPrepareExample = `# Prepare every node in the config for an install
$ cargoship prepare ./build/cargoship-distro-amd64.tar.zst --config ./cargoship-config.yaml --confirm

# Update /etc/hosts, the firewall, and fapolicyd on every node
$ cargoship prepare ./build/cargoship-distro-amd64.tar.zst --config ./cargoship-config.yaml --confirm -H -F -f

# Prepare at most five hosts at a time
$ cargoship prepare ./build/cargoship-distro-amd64.tar.zst --config ./cargoship-config.yaml --confirm --concurrency 5

# Prepare workers 25% at a time
$ cargoship prepare ./build/cargoship-distro-amd64.tar.zst --config ./cargoship-config.yaml --confirm --work-concurrency 25%`

	// CmdDistroResetExample reset example
	CmdDistroResetExample = `# Reset an RKE2 cluster, uninstalling the engine and removing its data
$ cargoship reset --config ./cargoship-config.yaml --distro rke2 --confirm

# Reset a K3s cluster
$ cargoship reset --config ./cargoship-config.yaml --distro k3s --confirm

# Reset at most five hosts at a time
$ cargoship reset --config ./cargoship-config.yaml --distro rke2 --confirm --concurrency 5

# Reset workers 25% at a time
$ cargoship reset --config ./cargoship-config.yaml --distro rke2 --confirm --work-concurrency 25%`

	// CmdDistroKubeConfigExample kube-config example
	CmdDistroKubeConfigExample = `# Fetch the admin kubeconfig from an RKE2 control-plane node
$ cargoship kube-config --config ./cargoship-config.yaml --distro rke2

# Fetch it from a K3s cluster
$ cargoship kube-config --config ./cargoship-config.yaml --distro k3s

# Use the distro set in the resolved cargoship config
$ cargoship kube-config --config ./cargoship-config.yaml`

	// CmdDistroEngineConfigSyncExample engine-config-sync example
	CmdDistroEngineConfigSyncExample = `# Sync registry, audit, and pod security config to every node that has drifted
$ cargoship engine-config-sync ./build/cargoship-distro-amd64.tar.zst --config ./cargoship-config.yaml --confirm

# Decrypt vault-encrypted registry credentials with a password file
$ cargoship engine-config-sync ./build/cargoship-distro-amd64.tar.zst --config ./cargoship-config.yaml --confirm --vault-password-file ./vault-pass.txt

# Restart at most 25% of the workers at a time
$ cargoship engine-config-sync ./build/cargoship-distro-amd64.tar.zst --config ./cargoship-config.yaml --confirm --work-concurrency 25%

# Sync without touching the local kubeconfig
$ cargoship engine-config-sync ./build/cargoship-distro-amd64.tar.zst --config ./cargoship-config.yaml --confirm --update-kubeconfig=false`

	// CmdDistroCreateExample create example
	CmdDistroCreateExample = `# Build a package from the definition in the current directory
$ cargoship create .

# Build from another directory, writing the package to ./build/
$ cargoship create ./distro-defs -o ./build/

# Pull images through an internal mirror instead of their upstream registry
$ cargoship create ./distro-defs --registry-override docker.io=mirror.example.com

# Sign the package as it is built, without prompting for the key password
$ cargoship create ./distro-defs --signing-key ./private-key.pem --confirm

# Build a byte-identical package on every run, skipping SBOM generation
$ cargoship create ./distro-defs --reproducible --skip-sbom`

	// CmdDistroPublishExample publish example
	CmdDistroPublishExample = `# Publish a package to an OCI registry
$ cargoship publish ./build/cargoship-rancher-rke2-amd64-1.0.0.tar.zst oci://ghcr.io/my-org

# Publish and re-sign the package with a different key
$ cargoship publish ./build/cargoship-rancher-rke2-amd64-1.0.0.tar.zst oci://ghcr.io/my-org --signing-key ./private-key.pem --confirm

# Retry failed layer uploads over a slow link
$ cargoship publish ./build/cargoship-rancher-rke2-amd64-1.0.0.tar.zst oci://ghcr.io/my-org --retries 3 --oci-concurrency 3

# Refuse to publish unless the package carries a signature this key validates
$ cargoship publish ./build/cargoship-rancher-rke2-amd64-1.0.0.tar.zst oci://ghcr.io/my-org --verify=always --key ./public-key.pem`

	// CmdPackagePullExample pull example
	CmdPackagePullExample = `# Pull a package into the current directory
$ cargoship pull oci://ghcr.io/my-org/my-package:1.0.0

# Pull it into ./build/ instead
$ cargoship pull oci://ghcr.io/my-org/my-package:1.0.0 --output ./build/

# Check the downloaded package against a known checksum
$ cargoship pull oci://ghcr.io/my-org/my-package:1.0.0 --shasum 4a4f1f5eb0a1e3f2c9b6d0b2b6a0d3f4c5e6a7b8c9d0e1f2a3b4c5d6e7f8a9b0

# Refuse to keep the package unless a signature validates against this key
$ cargoship pull oci://ghcr.io/my-org/my-package:1.0.0 --verify=always --key ./public-key.pem

# Require a keyless signature from a known identity and OIDC issuer
$ cargoship pull oci://ghcr.io/my-org/my-package:1.0.0 --verify=always --certificate-identity signer@example.com --certificate-oidc-issuer https://token.actions.githubusercontent.com`

	// CmdDistroSignExample sign example
	CmdDistroSignExample = `# Sign an unsigned package
$ cargoship sign cargoship-rancher-rke2-amd64-1.0.0.tar.zst --signing-key ./private-key.pem

# Re-sign with a new key (overwrite existing signature)
$ cargoship sign cargoship-rancher-rke2-amd64-1.0.0.tar.zst --signing-key ./new-key.pem --overwrite

# Sign a package from an OCI registry and output to a local directory
$ cargoship sign oci://ghcr.io/my-org/my-package:1.0.0 --signing-key ./private-key.pem --output ./signed/

# Sign a package and publish directly to an OCI registry
$ cargoship sign cargoship-rancher-rke2-amd64-1.0.0.tar.zst --signing-key ./private-key.pem --output oci://ghcr.io/my-org/signed-packages

# Sign with a cloud KMS key
$ cargoship sign cargoship-rancher-rke2-amd64-1.0.0.tar.zst --signing-key awskms://alias/my-signing-key`

	// CmdSha256SumExample sha256sum example
	CmdSha256SumExample = `# Checksum a local file
$ cargoship sha256sum ./build/cargoship-rancher-rke2-amd64-1.0.0.tar.zst

# Checksum a remote file, downloading it first
$ cargoship sha256sum https://example.com/artifact.tar.gz

# Checksum one file inside an archive, for use with a component's files.extractPath
$ cargoship sha256sum ./artifact.tar.gz --extract-path ./bin/tool

# Same thing, using the shorter alias
$ cargoship sum ./build/cargoship-rancher-rke2-amd64-1.0.0.tar.zst`

	// CmdVaultEncryptExample vault-encrypt example
	CmdVaultEncryptExample = `# Encrypt a registry password for a config file's user/pass/token field
$ cargoship vault-encrypt my-registry-password --vault-password-file ./vault-pass.txt

# Omit the value to be prompted for it, with the input hidden
$ cargoship vault-encrypt --vault-password-file ./vault-pass.txt

# Encrypt a value piped in on stdin
$ printf my-registry-password | cargoship vault-encrypt --vault-password-file ./vault-pass.txt

# Encrypt the contents of a file
$ cargoship vault-encrypt --vault-password-file ./vault-pass.txt < ./registry-token.txt`

	// CmdVersionExample version example
	CmdVersionExample = `# Print the version of the running binary
$ cargoship version

# Print the full build information as YAML
$ cargoship version --output yaml

# Print it as JSON, for piping into jq
$ cargoship version -o json`
)
