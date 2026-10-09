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
    kw1 = {
      address  = "10.3.20.22"
      role     = "worker"
      user     = "operator"
      key_path = "~/.ssh/bubbles"
      profile  = "general"
    }
  }

  modify_hosts       = true
  modify_firewall    = true
  label_nodes        = true
  worker_concurrency = "25%"
  timeout            = "30m"
}
