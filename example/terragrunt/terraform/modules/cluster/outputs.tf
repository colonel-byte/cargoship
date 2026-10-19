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

output "distro" {
  value       = cargoship_cluster.this.distro
  description = "The engine the package carried, read from the package rather than stated twice"
}

output "engine_version" {
  value       = cargoship_cluster.this.engine_version
  description = "The engine version the package carried"
}

output "nodes" {
  value       = cargoship_cluster.this.nodes
  description = "What each host reported during the run, controllers first"
}

output "kubeconfig" {
  value       = cargoship_cluster.this.kubeconfig
  description = "The cluster's admin credentials, empty unless export_kubeconfig was set"
  sensitive   = true
}
