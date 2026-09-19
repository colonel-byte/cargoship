#!/usr/bin/env bash
# Copyright 2026 colonel-byte
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

#
# Build the colonel_byte.cargoship Ansible collection for a release.
#
# GoReleaser runs this as a before hook, so the collection tarball exists by the time the release
# is assembled and can be attached to it. The tarball is what an operator installs with
# ansible-galaxy on a management node that has no network: the collection is not published to
# Galaxy, because a node inside an airlock cannot reach it.
#
# Usage:
#   hack/ansible-build-collection.sh <output-dir> [expected-version]
#
# expected-version, when given, must match the version in galaxy.yml. release-please keeps that
# version in step with the tag, and a mismatch means a release would publish a collection whose
# version is not the one being released.
#
# The tarball is signed when COSIGN_PRIVATE_KEY is set, with the key and bundle layout the rest of
# the release uses. It is left unsigned otherwise, which is what a local build wants.

set -euo pipefail

collection=ansible/colonel_byte/cargoship

output=${1:-}
expected=${2:-}

if [[ -z ${output} ]]; then
	echo "usage: $0 <output-dir> [expected-version]" >&2
	exit 2
fi
if ! command -v ansible-galaxy >/dev/null; then
	echo "$0: ansible-galaxy is not on PATH: install ansible-core to build the collection" >&2
	exit 1
fi

version=$(sed -n 's/^version:[[:space:]]*\([^[:space:]#]*\).*/\1/p' "${collection}/galaxy.yml")
if [[ -z ${version} ]]; then
	echo "$0: no version in ${collection}/galaxy.yml" >&2
	exit 1
fi
if [[ -n ${expected} && ${version} != "${expected}" ]]; then
	echo "$0: ${collection}/galaxy.yml says ${version}, but the release is ${expected}" >&2
	exit 1
fi

mkdir -p "${output}"
ansible-galaxy collection build --force --output-path "${output}" "${collection}"

tarball="${output}/colonel_byte-cargoship-${version}.tar.gz"
if [[ ! -f ${tarball} ]]; then
	echo "$0: ansible-galaxy did not produce ${tarball}" >&2
	exit 1
fi

if [[ -z ${COSIGN_PRIVATE_KEY:-} ]]; then
	echo "$0: COSIGN_PRIVATE_KEY is unset, leaving ${tarball} unsigned"
	exit 0
fi

cosign sign-blob \
	--key env://COSIGN_PRIVATE_KEY \
	--bundle "${tarball}.sigstore.json" \
	--yes \
	"${tarball}"
