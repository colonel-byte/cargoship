# The `colonel_byte.zarf` collection

Zarf runs inside a playbook as an Ansible collection: `zarf init` initialises a cluster and `zarf package deploy` puts a package on it, both driven from the node the task is delegated to. The modules are compiled wrapper binaries named `zarf_<action>`, and the name selects the action. There is no Python module behind either, and nothing is installed on the cluster's nodes.

This collection is a proof of [ZEP-0072](https://github.com/zarf-dev/proposals/pull/73), which proposes this pattern for zarf itself, built here because the pattern it copies is the one [`colonel_byte.cargoship`](../cargoship/collection.md) already runs. It is expected to be removed once that proposal is resolved -- see [choice-zarf-collection-removal](../../agent/choice-zarf-collection-removal.md).

Zarf is not an SSH orchestrator. It reaches one cluster through a kubeconfig, so Ansible supplies no inventory here: a task supplies a kubeconfig, a package, and the parameters the zarf command takes. The zarf that does the work is the one on the node's path, which is what keeps the module and the tool in step.

## What is here

| Page                                                | What it covers                                                                    |
| --------------------------------------------------- | --------------------------------------------------------------------------------- |
| [Modules](modules.md)                               | The actions: initialising a cluster, deploying packages, and inspecting state.   |
| [Roles](roles.md)                                   | Convenient role wrapper for fetching cluster state facts.                         |
| [Module guide](../../guides/zarf-ansible-module.md) | Installing the modules, the two-module walk, check mode, and the result.          |

## Which to reach for

There is no role here, and no module picks the other's work up. A playbook writes the action it wants: `zarf_init` for the cluster itself, `zarf_package_deploy` for anything deployed onto it afterwards.

The two are usually written together rather than separately. A cluster whose distribution ships no storage provider cannot be initialised until one is deployed -- zarf's registry claims a volume during the seed phase -- and a package cannot normally be deployed until the cluster is initialised. The way out is a connected deploy, which pushes no images and so needs no registry; the [module guide](../../guides/zarf-ansible-module.md) covers the ordering.

## Reference pages are generated

Every `module_*.md` page here is generated from the collection itself -- the `DOCUMENTATION` and `EXAMPLES` blocks in each action plugin -- by `mage generate:document`. Edit the collection and regenerate; an edit to a generated page does not survive the next commit.

This page and [modules.md](modules.md) are written by hand. The generators are shared with the cargoship collection, so the contract in `ansible/colonel_byte/cargoship/AGENTS.md` applies here too: every option declares a `type`, a `description`, and a `cli_flag`, and every `description` entry is double-quoted.
