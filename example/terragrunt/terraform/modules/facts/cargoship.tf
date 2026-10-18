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

# Read-only. It runs the five read-only phases of an apply, takes no cluster lock, and writes
# nothing to any host -- so it is safe to point at a fleet somebody else is responsible for, and
# it is what to reach for when a configuration needs to route on what the hosts are running.
data "cargoship_cluster_facts" "this" {
  name          = var.name
  load_balancer = var.load_balancer
  distro        = var.distro

  dynamic "host" {
    for_each = var.hosts
    content {
      address         = host.value.address
      role            = host.value.role
      user            = host.value.user
      port            = host.value.port
      key_path        = host.value.key_path
      profile         = host.value.profile
      hostname        = host.value.hostname
      private_address = host.value.private_address
    }
  }
}
