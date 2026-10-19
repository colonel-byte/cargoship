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
