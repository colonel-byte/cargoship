include {
  path = find_in_parent_folders("root.hcl")
}

terraform {
  source = "../../../modules/cluster/"
}

# A single-node cluster: one host carrying both roles, which is what the `single` role is for. It
# is the cheapest shape to try the provider against, and the one a laptop can hold.
inputs = {
  name          = "staging"
  load_balancer = "10.3.30.10"
  package       = "/srv/staging/k3s-v1.36.4+k3s1.tar.zst"

  hosts = {
    sn0 = {
      address  = "10.3.30.11"
      role     = "single"
      user     = "operator"
      key_path = "~/.ssh/staging"
    }
  }

  timeout = "20m"

  # A staging cluster is rebuilt often, so leave the workloads where they are on the way down.
  no_drain_on_destroy = true
}
