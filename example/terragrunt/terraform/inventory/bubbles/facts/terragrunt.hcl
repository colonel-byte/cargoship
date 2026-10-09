include {
  path = find_in_parent_folders("root.hcl")
}

terraform {
  source = "../../../modules/facts/"
}

# The facts are read after the cluster is converged, so the read reports the engine the apply
# installed rather than racing it. Reading a fleet nothing in this repository installs needs no
# dependency at all -- the data source changes nothing and takes no lock.
dependencies {
  paths = [
    "../cluster",
  ]
}

inputs = {
  name          = "bubbles"
  load_balancer = "10.3.20.10"
  distro        = "k3s"

  hosts = {
    kc0 = {
      address  = "10.3.20.11"
      role     = "controller"
      user     = "operator"
      key_path = "~/.ssh/bubbles"
    }
    kc1 = {
      address  = "10.3.20.12"
      role     = "controller"
      user     = "operator"
      key_path = "~/.ssh/bubbles"
    }
    kw0 = {
      address  = "10.3.20.21"
      role     = "worker"
      user     = "operator"
      key_path = "~/.ssh/bubbles"
    }
    kw1 = {
      address  = "10.3.20.22"
      role     = "worker"
      user     = "operator"
      key_path = "~/.ssh/bubbles"
    }
  }
}
