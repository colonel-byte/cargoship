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
# Prints the package name of every example flavor, one per line.
#
# publish-example.yaml publishes example/<flavor>/<minor>/<version>/distro.yaml to
# <registry>/<metadata.name>, so the package name is the name the definition carries, not the
# flavor directory: example/rke2-cilium publishes rancher-rke2-cilium. The pruner needs those
# names because GITHUB_TOKEN cannot list an organization's packages and can only act on a
# package it names.
#
# Only the newest version of each flavor is read. Older definitions under the same flavor
# carry the same metadata.name, so reading them all would only repeat it.
set -euo pipefail

root=${1:-example}

for flavor in "$root"/*/; do
  [ -d "$flavor" ] || continue

  dir=$(find "$flavor" -mindepth 2 -maxdepth 2 -type d | sort -V | tail -n 1)
  [ -n "$dir" ] && [ -f "$dir/distro.yaml" ] || continue

  name=$(yq -r '.metadata.name' "$dir/distro.yaml")
  if [ -z "$name" ] || [ "$name" = "null" ]; then
    echo "no metadata.name in $dir/distro.yaml" >&2
    exit 1
  fi
  echo "$name"
done | sort -u
