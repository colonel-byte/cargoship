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
# Resolve an Ansible inventory into the document `cargoship inventory from-ansible` reads.
#
# This is what the collection's action plugin does inside a playbook, done in the shell so that an
# inventory can be translated and checked without running a play or touching the fleet:
#
#   ./resolve-inventory.sh inventories/fleet bubbles bubbles-kc.test.com > resolved.json
#   cargoship inventory from-ansible resolved.json -o inventory.yaml
#   cargoship validate inventory.yaml
#
# Usage:
#   resolve-inventory.sh <inventory> <cluster-name> <loadbalancer> [role-groups-json]
#
# `ansible-inventory --list` writes groups as objects and host variables under _meta; cargoship
# reads a flat group-to-hosts map and a hostvars map, so the shapes are not the same document. The
# host variables are filtered to the ones cargoship reads, which is the same filter the action
# plugin applies: a host's resolved variables routinely carry credentials for things that are not
# this cluster, and the result of this script is a file on disk.
#

set -euo pipefail

inventory=${1:-}
name=${2:-}
loadbalancer=${3:-}
role_groups=${4:-}

if [[ -z ${inventory} || -z ${name} || -z ${loadbalancer} ]]; then
	echo "usage: $0 <inventory> <cluster-name> <loadbalancer> [role-groups-json]" >&2
	exit 2
fi
for tool in ansible-inventory jq; do
	if ! command -v "${tool}" >/dev/null; then
		echo "$0: ${tool} is not on PATH" >&2
		exit 1
	fi
done

ansible-inventory -i "${inventory}" --list | jq \
	--arg name "${name}" \
	--arg loadbalancer "${loadbalancer}" \
	--arg role_groups "${role_groups}" \
	'{
		groups: (
			to_entries
			| map(select(.key != "_meta"))
			| map({key: .key, value: (.value.hosts // [])})
			| from_entries
		),
		hostvars: (
			(._meta.hostvars // {})
			| map_values(
				with_entries(
					select(
						(.key | startswith("cargoship_"))
						or (.key | IN(
							"ansible_host",
							"ansible_port",
							"ansible_ssh_private_key_file",
							"ansible_user"
						))
					)
				)
			)
		),
		cluster: {name: $name, loadbalancer: $loadbalancer}
	} + if $role_groups != "" then {roleGroups: ($role_groups | fromjson)} else {} end'
