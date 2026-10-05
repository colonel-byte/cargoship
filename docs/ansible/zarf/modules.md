# Modules

Two modules ship, one per zarf command a convergence run needs. Package creation is not among them -- it builds an artifact from a definition in a repository, which belongs in a pipeline rather than in a convergence run -- and neither is package removal, which nothing here has needed yet.

| Module                                                   | What it does                                                                 |
| -------------------------------------------------------- | ---------------------------------------------------------------------------- |
| [`zarf_init`](module_init.md)                            | Initialises a cluster: the injector, the seed registry, the registry, the agent. |
| [`zarf_package_deploy`](module_package_deploy.md)        | Puts a package on a cluster, from a path, an `oci://` reference, or an `https://` URL. |
| [`zarf_package_info`](module_package_info.md)            | Queries installed packages via `zarf package list`. |
| [`zarf_package_inspect`](module_package_inspect.md)      | Inspects and parses the YAML definition of a Zarf package. |
| [`zarf_state_info`](module_state_info.md)                | Retrieves and parses the cluster's `zarf-state` Secret via `zarf tools kubectl`. |

## The surfaces differ because the commands do

`zarf init` configures the registry and git server the cluster will use, so it takes the registry and git parameters and no package. `zarf package deploy` names a package outright and takes none of that configuration, but takes `connected`, `namespace` and `shasum`, which only a deploy has. A parameter offered to the wrong module is an error naming it, rather than a flag quietly dropped.

Each module's page lists its full parameter set and the flag each parameter renders. A parameter that renders no flag is listed as `None`: it is either the command's positional argument, something passed to zarf in its environment, or something the action plugin consumes before the command line is built.

## What every task needs

A parameter left unset is left unset: the module passes no flag for it, so whatever zarf's own configuration file sets still applies. That is why `plain_http: false` and omitting `plain_http` are different.

Set `run_once: true` and `delegate_to` on every task. One `zarf init` initialises the whole cluster and one deploy puts the package on all of it, so a play over the cluster's own inventory would otherwise do the work once per host. Set `no_log: true` on any task carrying a credential: a binary module has no per-parameter `no_log`, so the task-level setting is what suppresses the arguments and the result.

Credentials given as parameters are rendered as flags, which puts them in the process table for the life of the run. `zarf_config` names a zarf configuration file instead, which zarf reads them from.

## What the result says

Both modules report `changed: true` with `changedSignal: partial` on every successful run, and name every component they deployed in `changedUndeclared`. Zarf reports which components ran and not whether any of them found the cluster already as it wanted, so `partial` is the honest answer rather than an unfinished one.

Check mode reports the task as skipped. Neither command has a dry run.

The [module guide](../../guides/zarf-ansible-module.md) covers installing the modules, the ordering a cluster with no storage provider needs, the live progress display, and the shape of the result.
