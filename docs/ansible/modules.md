# Modules

Five modules ship, one per fleet action. Package creation is not among them -- it builds an artifact from a definition in a repository, which belongs in a pipeline rather than in a convergence run.

| Module                                                         | What it does                                                         |
| -------------------------------------------------------------- | -------------------------------------------------------------------- |
| [`cargoship_apply`](module_apply.md)                           | Installs or converges the whole cluster. The widest action there is. |
| [`cargoship_prepare`](module_prepare.md)                       | Stages a package onto the fleet and readies the hosts.               |
| [`cargoship_engine_config_sync`](module_engine_config_sync.md) | Converges engine configuration across the fleet.                     |
| [`cargoship_reset`](module_reset.md)                           | Removes the cluster from the fleet.                                  |
| [`cargoship_kube_config`](module_kube_config.md)               | Fetches the cluster kubeconfig onto the management node.             |

## The surfaces differ because the commands do

`cargoship prepare` reads no encrypted value, so it takes no `vault_password_file`. `cargoship reset` installs nothing, so it takes no package and no `values`. `cargoship engine-config-sync` does not update the hosts themselves, so it takes none of the three host switches. A parameter offered to the wrong module is an error naming it, rather than a flag quietly dropped.

Each module's page lists its full parameter set and the flag each parameter renders. A parameter that renders no flag is listed as `None`: it is either the command's positional argument, or something consumed before the command line is built.

## What every task needs

A parameter left unset is left unset: the module passes no flag for it, so whatever your cargoship configuration file sets still applies. That is why `label_nodes: false` and omitting `label_nodes` are different.

Set `run_once: true`, `delegate_to`, and `no_log: true` on every direct module call. The [`cluster` role](role_cluster.md) sets all three for you. Key material is always named by a path and never given by value, because Ansible writes a module's parameters into a file on disk.

The [module guide](../guides/ansible-module.md) covers installing the collection, check mode, what `changed` is worth, and the shape of the result.
