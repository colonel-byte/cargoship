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

variable "name" {
  type        = string
  description = "The cluster's name. It becomes metadata.name and the kubeconfig context, and changing it replaces the cluster"
}

variable "load_balancer" {
  type        = string
  description = "The address clients use to reach the control plane"
}

variable "package" {
  type        = string
  description = "The distro package to install: a path to a .tar.zst built by `cargoship package create`, or an OCI reference"
}

variable "profiles" {
  type = map(object({
    node_labels = optional(map(string), null)
    node_taints = optional(list(string), null)
    ports = optional(list(object({
      port     = string
      protocol = optional(string, null)
    })), null)
    firewall_rules = optional(list(object({
      action      = string
      name        = optional(string, null)
      direction   = optional(string, null)
      source      = optional(string, null)
      destination = optional(string, null)
      ingress     = optional(string, null)
      egress      = optional(string, null)
      port        = optional(string, null)
      protocol    = optional(string, null)
    })), null)
    concurrency = optional(string, null)
  }))
  description = "What each profile means, keyed by name. A host selects one by name, and a host selecting a profile this map does not define is a configuration error"
  default     = {}
}

variable "hosts" {
  type = map(object({
    address         = string
    role            = string
    user            = optional(string, null)
    port            = optional(number, null)
    key_path        = optional(string, null)
    profile         = optional(string, null)
    hostname        = optional(string, null)
    private_address = optional(string, null)

    private_interface = optional(string, null)
    environment       = optional(map(string), null)
    node_labels       = optional(map(string), null)
    node_taints       = optional(list(string), null)
    ports = optional(list(object({
      port     = string
      protocol = optional(string, null)
    })), null)
    firewall_rules = optional(list(object({
      action      = string
      name        = optional(string, null)
      direction   = optional(string, null)
      source      = optional(string, null)
      destination = optional(string, null)
      ingress     = optional(string, null)
      egress      = optional(string, null)
      port        = optional(string, null)
      protocol    = optional(string, null)
    })), null)
    bastion = optional(object({
      address  = string
      user     = optional(string, null)
      port     = optional(number, null)
      key_path = optional(string, null)
    }), null)
  }))
  description = "The fleet, keyed by a name of your choosing. key_path is a path on the machine running OpenTofu; there is no attribute for key material, because a key in the configuration is a key in the state file"
}

variable "concurrency" {
  type        = number
  description = "How many hosts a phase acts on at once. Zero is unlimited"
  default     = 0
}

variable "connect_timeout" {
  type        = string
  description = "How long to wait for the fleet to answer, as a Go duration. cargoship's connect phase retries for ten minutes, which is the wrong answer for a plan"
  default     = "1m"
}

variable "modify_hosts" {
  type        = bool
  description = "Rewrite /etc/hosts on every host with the cluster's own nodes"
  default     = false
}

variable "modify_firewall" {
  type        = bool
  description = "Rewrite the host firewall (firewalld, ufw or nftables) with the engine's rules"
  default     = false
}

variable "label_nodes" {
  type        = bool
  description = "Add the node-role.kubernetes.io/<profile> label to each node, from the host's profile"
  default     = false
}

variable "worker_concurrency" {
  type        = string
  description = "How many workers are installed or upgraded at a time, as a count (5) or a percentage of the batch (25%)"
  default     = null
}

variable "allow_unmanaged_nodes" {
  type        = bool
  description = "Continue when the cluster holds a node no host accounts for. An apply removes nothing, so by default a node left behind by a deleted host stops the run"
  default     = false
}

variable "allow_downgrade" {
  type        = bool
  description = "Continue when a host already runs an engine newer than the package carries"
  default     = false
}

variable "timeout" {
  type        = string
  description = "How long the phases wait for a host to reach the state they want, as a Go duration"
  default     = "20m"
}

variable "export_kubeconfig" {
  type        = bool
  description = "Return the cluster's admin credentials in the kubeconfig output. Leave it off unless something consumes them"
  default     = false
}

variable "retain_on_destroy" {
  type        = bool
  description = "Leave the cluster running when the resource is destroyed, dropping it from state with a warning instead of resetting it"
  default     = false
}

variable "no_drain_on_destroy" {
  type        = bool
  description = "Skip draining each node before it is deleted during a destroy"
  default     = false
}
