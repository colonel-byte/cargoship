variable "name" {
  type        = string
  description = "The cluster's name, which becomes metadata.name in the document cargoship reads"
}

variable "load_balancer" {
  type        = string
  description = "The address clients use to reach the control plane. Nothing is contacted through it here -- the facts are read over SSH -- but the cluster document requires it"
}

variable "distro" {
  type        = string
  description = "The engine to read: k3s or rke2. It is what tells cargoship which version string to look for on each host"
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
  }))
  description = "The fleet to read, keyed by a name of your choosing"
}

variable "concurrency" {
  type        = number
  description = "How many hosts the phases read at once. Zero is unlimited"
  default     = 0
}

variable "connect_timeout" {
  type        = string
  description = "How long to wait for the fleet to answer, as a Go duration"
  default     = "1m"
}
