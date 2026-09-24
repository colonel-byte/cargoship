# The `colonel_byte.cargoship` collection

Cargoship runs inside a playbook as an Ansible collection. The cargoship binary *is* the module: it is installed as a set of symlinks named `cargoship_<action>`, and a symlink's name selects the action. There is no separate module package and nothing to install on the managed nodes.

Ansible supplies the inventory and nothing else. It does not connect to the fleet, gather facts on it, or run a task per host. Every task in this collection runs on one management node outside the cluster -- the node the package and images were staged onto -- and cargoship opens every SSH connection itself from there. See [choice-ansible-module](../agent/choice-ansible-module.md) for why the work is split that way.

That boundary is what the collection is for, and it is the whole of what the collection is for. A role that configures fleet hosts directly belongs in the playbook that consumes this collection, not in it.

## What is here

| Page                                        | What it covers                                                    |
| ------------------------------------------- | ----------------------------------------------------------------- |
| [Modules](modules.md)                       | The five execution primitives, one per fleet action.              |
| [Roles](roles.md)                           | `cluster`, the shorter way to write the same task.                |
| [Module guide](../guides/ansible-module.md) | Installing the collection, check mode, and what the result means. |
| [Inventory guide](../guides/ansible-inv.md) | The inventory document the modules take.                          |

## Which to reach for

Reach for the [`cluster` role](role_cluster.md) first. It picks the module from `cargoship_action`, assembles the `inventory` parameter, and sets `run_once`, `delegate_to`, and `no_log` for you.

Reach for a module directly when you need control the role does not give you. Then you must set `run_once: true` yourself, or the cluster is converged once per host in the fleet, and `delegate_to` and `no_log: true` with it.

## Reference pages are generated

Every `module_*.md` and `role_*.md` page here is generated from the collection itself -- a role's interface from its `meta/argument_specs.yml`, a module's from the `DOCUMENTATION` and `EXAMPLES` blocks in its action plugin -- by `mage generate:document`. Edit the collection and regenerate; an edit to a generated page does not survive the next commit.

This page, [modules.md](modules.md), and [roles.md](roles.md) are written by hand. See `ansible/colonel_byte/cargoship/AGENTS.md` for the contract.
