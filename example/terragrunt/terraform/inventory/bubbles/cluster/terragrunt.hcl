// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

include {
  path = find_in_parent_folders("root.hcl")
}

terraform {
  source = "../../../modules/cluster/"
}

inputs = {
  name          = "bubbles"
  load_balancer = "10.3.20.10"

  # A path, not an OCI reference: the management node this runs from is airgapped, so the package
  # is staged ahead of time by whatever carried it through the airlock.
  package = "/srv/staging/k3s-v1.36.4+k3s1.tar.zst"

  # What each profile means, stated once rather than per host. A host that selects a profile this
  # map does not define is a configuration error, which is what stops a typo from becoming a
  # taint that silently never exists.
  profiles = {
    control = {
      node_labels = { "adrp.xyz/purpose-control" = "true" }
      ports = [
        { port = "6443" },
      ]
      # Controllers come up one at a time: an embedded etcd cluster needs its quorum to form
      # before the next member joins.
      concurrency = "1"
    }

    general = {}

    infra = {
      node_taints = ["adrp.xyz/infra=true:NoSchedule"]
      firewall_rules = [
        {
          name     = "allow-backup"
          action   = "allow"
          source   = "10.0.0.0/8"
          port     = "2049"
          protocol = "tcp"
        },
      ]
    }
  }

  # key_path is a path on the machine running OpenTofu. There is no attribute for key material,
  # and there will not be: a key in the configuration is a key in the state file.
  hosts = {
    kc0 = {
      address  = "10.3.20.11"
      role     = "controller"
      user     = "operator"
      key_path = "~/.ssh/bubbles"
      profile  = "control"
    }
    kc1 = {
      address  = "10.3.20.12"
      role     = "controller"
      user     = "operator"
      key_path = "~/.ssh/bubbles"
      profile  = "control"
    }
    kw0 = {
      address  = "10.3.20.21"
      role     = "worker"
      user     = "operator"
      key_path = "~/.ssh/bubbles"
      profile  = "general"
    }
    # The infra node: it takes the infra profile's taint, and states its own label and its own
    # second interface. Its own node_labels replace whatever the profile sets rather than merging
    # with them, so everything this host wants labelled is written here.
    kw1 = {
      address           = "10.3.20.22"
      role              = "worker"
      user              = "operator"
      key_path          = "~/.ssh/bubbles"
      profile           = "infra"
      private_interface = "ens224"
      node_labels       = { "adrp.xyz/purpose-infra" = "true" }
      environment       = { NO_PROXY = "10.0.0.0/8,.bubbles.test" }
    }

    # A node behind a jump host. Cargoship opens its own SSH connections, so the bastion is
    # stated here rather than inherited from an SSH client configuration.
    kw2 = {
      address  = "10.3.40.21"
      role     = "worker"
      user     = "operator"
      key_path = "~/.ssh/bubbles"
      profile  = "general"
      bastion = {
        address  = "10.3.20.1"
        user     = "jump"
        key_path = "~/.ssh/bubbles-jump"
      }
    }
  }

  modify_hosts       = true
  modify_firewall    = true
  label_nodes        = true
  worker_concurrency = "25%"
  timeout            = "30m"
}
