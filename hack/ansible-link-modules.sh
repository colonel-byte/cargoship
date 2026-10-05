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
# Create the cargoship Ansible module files in an installed collection.
#
# The modules are symlinks to the cargoship binary: invoked under a cargoship_ name it answers
# Ansible instead of running the CLI. The distribution packages create these links themselves, so
# this script is for a collection installed from Galaxy or from a checkout.
#
# Usage:
#   hack/ansible-link-modules.sh <collection-root> [cargoship-binary]
#
# where <collection-root> is the installed or source collection directory, for example
# ~/.ansible/collections/ansible_collections/colonel_byte/cargoship

set -euo pipefail

ACTIONS=(apply engine_config_sync kube_config prepare reset)

root=${1:-}
binary=${2:-$(command -v cargoship || true)}

if [[ -z ${root} ]]; then
	echo "usage: $0 <collection-root> [cargoship-binary]" >&2
	exit 2
fi
# An installed collection has a MANIFEST.json and no galaxy.yml; a source checkout is the other
# way round. Accept either, so the same script serves a Galaxy install and a working tree.
if [[ ! -f ${root}/MANIFEST.json && ! -f ${root}/galaxy.yml ]]; then
	echo "$0: ${root} does not look like a collection: no MANIFEST.json and no galaxy.yml" >&2
	exit 1
fi
if [[ -z ${binary} ]]; then
	echo "$0: no cargoship binary found on PATH, and none given" >&2
	exit 1
fi
if [[ ! -x ${binary} ]]; then
	echo "$0: ${binary} is not executable" >&2
	exit 1
fi

binary=$(readlink -f "${binary}")
mkdir -p "${root}/plugins/modules"

for action in "${ACTIONS[@]}"; do
	ln -sfn "${binary}" "${root}/plugins/modules/cargoship_${action}"
	echo "cargoship_${action} -> ${binary}"
done
