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

# The root module the tofu e2e suite runs. It is copied into a temp directory beside a symlink
# named `module`, pointing at example/terragrunt/terraform/modules/cluster, and the fleet, the
# package and the per-step settings arrive as a generated terraform.tfvars.json.
#
# It consumes the shipped module rather than declaring the resource itself on purpose. A bare
# resource would test the provider and nothing else; going through the module also checks that
# the module's variable types still accept what the provider's schema wants, which is the half
# that breaks silently when an attribute changes shape.
#
# Nothing here is contacted at plan time. The addresses the variables carry are the bootloose
# containers the suite started, reached on 127.0.0.1 through published SSH ports.

module "cluster" {
  source = "./module"

  name          = var.name
  load_balancer = var.load_balancer
  package       = var.package
  hosts         = var.hosts

  # The settings whose effect is visible on the hosts or in the cluster, so that the attributes
  # the provider passes to the actions are exercised rather than left on their defaults.
  modify_hosts       = true
  label_nodes        = true
  worker_concurrency = "1"
  export_kubeconfig  = true

  timeout         = var.timeout
  connect_timeout = var.connect_timeout
}

variable "name" {
  type        = string
  description = "The cluster's name, which is the resource's identity"
}

variable "load_balancer" {
  type        = string
  description = "The address the nodes reach the cluster through: the first controller's docker-bridge address"
}

variable "package" {
  type        = string
  description = "Path to the distro package the suite built"
}

variable "hosts" {
  type = map(object({
    address  = string
    role     = string
    user     = optional(string, null)
    port     = optional(number, null)
    key_path = optional(string, null)
    hostname = optional(string, null)
    profile  = optional(string, null)
    state    = optional(string, null)
  }))
  description = "The fleet, keyed by machine name. A host marked `state = \"absent\"` is the removal the suite walks"
}

variable "timeout" {
  type        = string
  description = "How long a phase waits on a host"
  default     = "20m"
}

variable "connect_timeout" {
  type        = string
  description = "How long the connect phase waits for a host to answer"
  default     = "2m"
}

output "distro" {
  value       = module.cluster.distro
  description = "The engine the package carried"
}

output "engine_version" {
  value       = module.cluster.engine_version
  description = "The engine version the package carried"
}

output "nodes" {
  value       = module.cluster.nodes
  description = "What each host reported during the run"
}

output "kubeconfig" {
  value       = module.cluster.kubeconfig
  description = "The cluster's admin credentials, which the suite writes to KUBECONFIG"
  sensitive   = true
}
