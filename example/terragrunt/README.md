# Terragrunt example

A worked layout for driving cargoship from Terragrunt: reusable OpenTofu modules under `terraform/modules/`, one directory per cluster under `terraform/inventory/`, and nothing in a module that names a cluster.

It is hand-written, unlike almost everything else under `example/` -- see [AGENTS.md](../AGENTS.md). Nothing here is generated, and nothing regenerates it.

> The provider is not published to a registry. Everything below needs a `filesystem_mirror` or an `oci_mirror` pointing at a build of it; [docs/dev/tofu-provider.md](../../docs/dev/tofu-provider.md) is how to get one, and [.tofurc.example](.tofurc.example) is the configuration that finds it.

## Layout

```
terraform/
  modules/
    cluster/        # cargoship_cluster: installs, joins and upgrades a cluster
    facts/          # cargoship_cluster_facts: reads a fleet, changes nothing
  inventory/
    root.hcl        # the root every leaf includes
    bubbles/        # a four-node cluster
      cluster/
      facts/
    staging/        # a single-node cluster
      cluster/
```

The root is `root.hcl` rather than `terragrunt.hcl`: current Terragrunt warns that a `terragrunt.hcl` at the root of a tree is an anti-pattern and will become an error, because it makes the root indistinguishable from a unit. That is also what the old `skip = true` papered over, and that argument has since been removed.

A module takes a fleet and settings and knows nothing about which cluster it is building. An inventory leaf is the cluster: a `terragrunt.hcl` naming the module, its dependencies, and its inputs. That split is what lets a second cluster be a directory rather than a copy of the module.

### Inventories

| Inventory | What it is                                                                                 |
| :-------- | :----------------------------------------------------------------------------------------- |
| `bubbles` | Four nodes, two controllers, labelled and firewalled. The shape a real cluster has         |
| `staging` | One node carrying both roles, through the `single` role. The cheapest thing to try against |

`bubbles` holds two leaves. `cluster` converges the cluster; `facts` reads it back afterwards and depends on `cluster` so the read reports what the apply installed rather than racing it. A fleet that nothing here installs needs no dependency: the data source takes no lock and changes nothing.

## Running it

Terragrunt drives OpenTofu rather than Terraform here, which it needs to be told:

```sh
export TG_TF_PATH=tofu
```

```sh
cd terraform/inventory/bubbles/cluster
terragrunt plan
terragrunt apply
```

Or the whole inventory, in dependency order:

```sh
cd terraform/inventory
terragrunt run-all plan
```

`run-all apply` works too, and is worth thinking about before using: every leaf's apply is a cluster installation, and `run-all` runs the independent ones concurrently. Each cluster takes its own lock on its own hosts, so they do not contend -- but a failure in one is reported alongside the output of the others.

## Credentials

The provider takes **no credentials as attributes**, deliberately: a value an attribute carries is a value the state file carries. See [choice-tofu-secrets](../../docs/agent/choice-tofu-secrets.md).

| What                         | Where it comes from                                                                     |
| :--------------------------- | :-------------------------------------------------------------------------------------- |
| SSH private key              | `key_path` on a host entry -- a path on the machine running OpenTofu, never the key     |
| Ansible Vault password       | `CARGOSHIP_VAULT_PASSWORD`, or `ANSIBLE_VAULT_PASSWORD`                                 |
| age identity                 | `CARGOSHIP_AGE_IDENTITY_FILE`                                                           |
| Cluster admin credentials    | The `kubeconfig` output, only when `export_kubeconfig` is set                           |

The environment is where a secrets manager hands them over. With [sops](https://github.com/getsops/sops), which is what the layout this example follows uses for everything else:

```sh
sops exec-env secrets.enc.yml 'terragrunt apply'
```

`export_kubeconfig` is off in every leaf here, and should stay off unless something in the configuration consumes the credentials. Any computed attribute lands in the state file whether or not it is marked sensitive, and those credentials are cluster-admin; `cargoship install kube-config` writes a kubeconfig without putting one in state.

## Things worth knowing before the first apply

**An apply is long and looks silent.** The phases log through cargoship's own logger, and OpenTofu shows the resource as "Still creating..." until the whole run returns. `TF_LOG=debug` is how to watch it.

**A failed apply still writes state, on purpose.** An apply that failed part way through has already changed hosts, so the resource records what it reached and reports the error alongside it. A resource that wrote nothing would leave the next plan deciding nothing is installed, and re-bootstrapping a live cluster.

**Removing a host entry does not remove the node.** An apply reconciles nothing downwards: the machine keeps running and stays joined, and the next apply stops rather than walking past it. `allow_unmanaged_nodes` downgrades that to a warning. The reasoning is in [choice-removed-hosts](../../docs/agent/choice-removed-hosts.md), and the short version is that a configuration no longer describing a host also no longer describes how to reach it.

**A destroy resets the cluster.** `retain_on_destroy` drops the resource and leaves the cluster running instead, with a warning naming it.

**There is no `import`.** A cluster's host entries and their key paths are not recoverable from the cluster itself, so an existing cluster cannot be adopted into state. Read it with the `facts` module instead.
