# Driving Cargoship from OpenTofu and Terragrunt

This guide covers installing the cargoship OpenTofu provider, the layout that keeps one module serving many clusters, and the four behaviours that make this resource different from the ones a practitioner is used to. The worked layout it describes is in [`example/terragrunt/`](https://github.com/colonel-byte/cargoship/tree/main/example/terragrunt); [tofu-provider](../dev/tofu-provider.md) is the developer-facing page for building and debugging the provider itself.

The provider is early. A `cargoship_cluster` resource converges a cluster and a `cargoship_cluster_facts` data source reads one, and what is not there yet is named at the end.

## Installing the provider

The provider is not published to a registry, and for an airgapped site it would not matter if it were: `tofu init` on a management node with no route out resolves nothing from `registry.opentofu.org`. Both ways in are mirrors, declared in the CLI configuration rather than in a module.

A directory of provider binaries:

```hcl
# ~/.tofurc, or anywhere named by TF_CLI_CONFIG_FILE
provider_installation {
  filesystem_mirror {
    path    = "/srv/staging/tofu-providers"
    include = ["registry.opentofu.org/colonel-byte/*"]
  }

  direct {
    exclude = ["registry.opentofu.org/colonel-byte/*"]
  }
}
```

Or an OCI registry, which is the one a site with an internal registry already has:

```hcl
provider_installation {
  oci_mirror {
    repository_template = "registry.bubbles.test/tofu-providers/${namespace}/${type}"
    include             = ["registry.opentofu.org/colonel-byte/*"]
  }
}
```

The `direct` block with the matching exclusion is what stops OpenTofu reaching for the registry for this provider while leaving every other provider alone. The registry hostname stays in the source address either way: a mirror redirects an address rather than replacing it, so a configuration still names `colonel-byte/cargoship`.

A `filesystem_mirror` expects the binary at `<path>/<hostname>/<namespace>/<type>/<version>/<os>_<arch>/terraform-provider-cargoship_v<version>`. Two things about that bite once each: the version has to be one OpenTofu accepts -- `0.0.0` is refused, since it is reserved for a provider that is not published -- and `tofu init` records the binary's checksum in `.terraform.lock.hcl`, so a replaced binary fails the next plan until the lock file is deleted and `init` re-run.

## The layout

A module that names a cluster is a module you copy to get a second cluster. So the cluster lives in the inventory and the module takes a fleet:

```
terraform/
  modules/
    cluster/        # cargoship_cluster
    facts/          # cargoship_cluster_facts
  inventory/
    root.hcl        # what the whole inventory shares
    bubbles/
      cluster/      # terragrunt.hcl: source, dependencies, inputs
      facts/
    staging/
      cluster/
```

A leaf is small, because everything structural is in the module:

```hcl
include {
  path = find_in_parent_folders("root.hcl")
}

terraform {
  source = "../../../modules/cluster/"
}

inputs = {
  name          = "bubbles"
  load_balancer = "10.3.20.10"
  package       = "/srv/staging/k3s-v1.36.4+k3s1.tar.zst"

  hosts = {
    kc0 = { address = "10.3.20.11", role = "controller", key_path = "~/.ssh/bubbles" }
    kw0 = { address = "10.3.20.21", role = "worker", key_path = "~/.ssh/bubbles" }
  }
}
```

Two conventions worth copying. The root is `root.hcl`, not `terragrunt.hcl`: current Terragrunt warns that a `terragrunt.hcl` at the root of a tree is an anti-pattern and will become an error, because it makes the root indistinguishable from a unit -- and the `skip = true` that used to paper over that has been removed outright. And Terragrunt has to be told to drive OpenTofu rather than Terraform:

```sh
export TG_TF_PATH=tofu
```

A `facts` leaf that reads a cluster this inventory also installs declares a dependency on the `cluster` leaf, so the read reports what the apply installed rather than racing it. Reading a fleet nothing here installs needs no dependency at all: the data source takes no lock and changes nothing.

## Profiles and per-host overrides

A fleet is not uniform. Controllers want a port open and a label; infra nodes want a taint so ordinary workloads stay off them; one node has a second interface or sits behind a jump host. A `profiles` map says what a profile means once, and a host selects one by name:

```hcl
resource "cargoship_cluster" "prod" {
  # ...

  profiles = {
    control = {
      node_labels = { "adrp.xyz/purpose-control" = "true" }
      ports       = [{ port = "6443" }]
      concurrency = "1"
    }

    general = {}

    infra = {
      node_taints = ["adrp.xyz/infra=true:NoSchedule"]
      firewall_rules = [
        { name = "allow-backup", action = "allow", source = "10.0.0.0/8", port = "2049" },
      ]
    }
  }

  hosts = {
    kc01 = {
      address  = "10.3.20.11"
      role     = "controller"
      key_path = "~/.ssh/bubbles"
      profile  = "control"
    }

    kw03 = {
      address           = "10.3.20.22"
      role              = "worker"
      key_path          = "~/.ssh/bubbles"
      profile           = "infra"
      private_interface = "ens224"
      node_labels       = { "adrp.xyz/purpose-infra" = "true" }
      environment       = { NO_PROXY = "10.0.0.0/8" }
    }
  }
}
```

`concurrency` on a profile is how many of the hosts sharing it cargoship acts on at once -- draining, upgrading, initializing, uninstalling -- as a count or a percentage of that group. `control = { concurrency = "1" }` is why an embedded etcd cluster forms its quorum before the next member joins.

Three rules decide what a host ends up with, and they are not all the same:

| Field                              | A host's own value...                      |
| :--------------------------------- | :----------------------------------------- |
| `node_labels`, `node_taints`, `ports` | **replaces** the profile's                |
| `firewall_rules`                   | is **unioned** with the profile's          |
| everything else                    | belongs to the host alone                  |

So a host that sets one label does not inherit the profile's others. That is `cluster.ZarfHostEngine.Merge`'s behaviour rather than a provider choice, and it matches what the Ansible collection does with `cargoship_node_labels` in `host_vars`.

**A host selecting a profile the map does not define is a configuration error.** That matters more than it sounds: nothing downstream treats an undefined profile as wrong. The lookup returns a zero value, the concurrency falls back to the phase's own, and the taint the operator wrote simply never exists -- a cluster that looks configured and is not. A `profile` with **no** `profiles` map at all is still fine, and still useful: it names the `node-role.kubernetes.io/<profile>` label `label_nodes` writes, and still groups hosts for concurrency.

A host behind a jump host states it, because cargoship opens its own SSH connections and inherits nothing from an SSH client configuration:

```hcl
kw04 = {
  address  = "10.3.40.21"
  role     = "worker"
  key_path = "~/.ssh/bubbles"

  bastion = {
    address  = "10.3.20.1"
    user     = "jump"
    key_path = "~/.ssh/bubbles-jump"
  }
}
```

The facts data source has none of this, and it still takes `host` blocks rather than a map, because a read has nothing to key. Its block takes an address, a role and the connection details, because a read needs nothing else -- so a fleet described for an apply is a superset of one described for a read.

## Package values

A package ships with values, and a cluster usually wants some of them changed. Two attributes do that, and they are merged in order -- the package's own values first, then `values`, then each file in `values_files` -- so the last one to set a key wins:

```hcl
resource "cargoship_cluster" "prod" {
  # ...

  values = yamlencode({
    cilium = {
      hubble = { enabled = true }
    }
  })

  values_files = ["/srv/staging/values/bubbles.yaml"]
}
```

`values` is a YAML string, so `yamlencode(...)` and `file("values.yaml")` both work; `values_files` are paths read on the machine running OpenTofu when the apply runs, not at plan time. What they override is the same `.spec.config.values` a cluster inventory carries, and the package's own `values.schema.json` is what validates the result -- so a key the package does not define fails the apply rather than being ignored.

## Credentials

The provider takes **no credentials as attributes**. A value an attribute carries is a value the state file carries, and a state file is committed, pushed to a remote backend, and readable by everyone with access to that backend. [choice-tofu-secrets](../agent/choice-tofu-secrets.md) records the reasoning.

| What                      | Where it comes from                                                                |
| :------------------------ | :--------------------------------------------------------------------------------- |
| SSH private key           | `key_path` on a host entry -- a path on the machine running OpenTofu, never the key |
| Ansible Vault password    | `CARGOSHIP_VAULT_PASSWORD`, or `ANSIBLE_VAULT_PASSWORD`                            |
| age identity              | `CARGOSHIP_AGE_IDENTITY_FILE`                                                      |
| Cluster admin credentials | The `kubeconfig` attribute, only when `export_kubeconfig` is set                    |

So the environment is where a secrets manager hands them over, and `sops exec-env secrets.enc.yml 'terragrunt apply'` is one way to do that.

Leave `export_kubeconfig` off unless something in the configuration consumes the credentials. Any computed attribute lands in the state file whether or not it is marked sensitive, and these are cluster-admin; `cargoship install kube-config` writes a kubeconfig without putting one in state.

## Four behaviours to know before the first apply

**One resource holds the whole cluster.** Cargoship takes a cluster-wide lock and starts controllers in a batch of one for quorum, so a resource per node would contend on that lock and lose the ordering the batching expresses. Adding a node is an entry in `hosts`, not a second resource.

**A failed apply still writes state.** An apply that failed part way through has already changed hosts, so the resource records what it reached and reports the error alongside it. The alternative -- writing nothing -- would leave the next plan deciding nothing is installed, and re-bootstrapping a live cluster. Read the error, fix the cause, and apply again: the phases are convergent, so the second run resumes rather than restarting.

**Removing a `host` block does not remove the node.** An apply converges forwards only: the machine keeps running the engine and stays joined, and the next apply stops rather than walking past it. `allow_unmanaged_nodes = true` downgrades that to a warning. [Removing a host from the cluster](removing-hosts.md) is the full account, and the short version is that a configuration which no longer describes a host also no longer describes how to reach it.

**A destroy resets the cluster.** `tofu destroy` drains the nodes, deletes them, and uninstalls the engine, because that is what destroy means everywhere else. `retain_on_destroy = true` drops the resource and leaves the cluster running instead, with a warning naming the cluster and the `cargoship install reset` that would tear it down.

## What is not there yet

- **No plan-time version or removal checks.** A lowered engine version and a deleted `hosts` entry are both caught during the apply, by cargoship's own phases, rather than during the plan. The `engine_version` each node reports is already in state, which is what the plan-time comparison will read.
- **No `import`.** A cluster's host entries and their key paths are not recoverable from the cluster itself, so an existing cluster cannot be adopted into state. Read it with the facts data source instead.
- **An apply looks silent.** The phases log through cargoship's own logger and OpenTofu shows the resource as "Still creating..." until the whole run returns. `TF_LOG=debug` is how to watch one.
