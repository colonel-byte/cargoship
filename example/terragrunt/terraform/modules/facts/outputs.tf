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

output "nodes" {
  value       = data.cargoship_cluster_facts.this.nodes
  description = "What each host reported: operating system, version, architecture, hostname, private address, and the engine version it is running"
}

output "engine_versions" {
  value       = { for node in data.cargoship_cluster_facts.this.nodes : node.hostname => node.engine_version }
  description = "The engine version per host, keyed by the hostname each one reported. v0.0.0 is a host running no engine"
}
