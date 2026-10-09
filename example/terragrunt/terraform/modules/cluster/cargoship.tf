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

provider "cargoship" {
  concurrency     = var.concurrency
  connect_timeout = var.connect_timeout
}

resource "cargoship_cluster" "this" {
  name          = var.name
  load_balancer = var.load_balancer
  package       = var.package

  modify_hosts          = var.modify_hosts
  modify_firewall       = var.modify_firewall
  label_nodes           = var.label_nodes
  worker_concurrency    = var.worker_concurrency
  allow_unmanaged_nodes = var.allow_unmanaged_nodes
  allow_downgrade       = var.allow_downgrade
  timeout               = var.timeout

  # Off unless something consumes it. Any computed attribute lands in the state file whether or
  # not it is marked sensitive, and these credentials are cluster-admin. `cargoship install
  # kube-config` writes a kubeconfig without putting one in state.
  export_kubeconfig = var.export_kubeconfig

  retain_on_destroy   = var.retain_on_destroy
  no_drain_on_destroy = var.no_drain_on_destroy

  hosts = var.hosts
}
