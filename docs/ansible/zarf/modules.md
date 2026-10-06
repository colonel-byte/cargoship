# Modules

Five modules ship. Two of them converge a cluster, and three read it.

`zarf_init` and `zarf_package_deploy` are the converging pair: they are the proof of [ZEP-0072](https://github.com/zarf-dev/proposals/pull/73), each is a compiled wrapper binary with its parameters and command line in Go, and [choice-zarf-ansible-module](../../agent/choice-zarf-ansible-module.md) records how. Package creation is not among them -- it builds an artifact from a definition in a repository, which belongs in a pipeline rather than in a convergence run -- and neither is package removal, which nothing here has needed yet.

`zarf_package_info`, `zarf_package_inspect` and `zarf_state_info` read. They change nothing, they answer during a check-mode play rather than skipping, and they are plain action plugins with no wrapper binary behind them, for the reasons in [choice-zarf-info-modules](../../agent/choice-zarf-info-modules.md).

| Module                                              | What it does                                                                              |
| --------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| [`zarf_init`](module_init.md)                       | Initialises a cluster: the injector, the seed registry, the registry, the agent.          |
| [`zarf_package_deploy`](module_package_deploy.md)   | Puts a package on a cluster, from a path, an `oci://` reference, or an `https://` URL.    |
| [`zarf_package_info`](module_package_info.md)       | Lists the packages deployed to a cluster, via `zarf package list`.                        |
| [`zarf_package_inspect`](module_package_inspect.md) | Reads the `zarf.yaml` of a package tarball, an `oci://` reference, or a deployed package. |
| [`zarf_state_info`](module_state_info.md)           | Reads the cluster's `zarf-state` Secret, withholding the credentials in it.               |

## The surfaces differ because the commands do

`zarf init` configures the registry and git server the cluster will use, so it takes the registry and git parameters and no package. `zarf package deploy` names a package outright and takes none of that configuration, but takes `connected`, `namespace` and `shasum`, which only a deploy has. A parameter offered to the wrong module is an error naming it, rather than a flag quietly dropped.

Each module's page lists its full parameter set and the flag each parameter renders. A parameter that renders no flag is listed as `None`: it is either the command's positional argument, something passed to zarf in its environment, or something the action plugin consumes before the command line is built.

## What every task needs

A parameter left unset is left unset: the module passes no flag for it, so whatever zarf's own configuration file sets still applies. That is why `plain_http: false` and omitting `plain_http` are different.

Set `run_once: true` and `delegate_to` on every task. One `zarf init` initialises the whole cluster, one deploy puts the package on all of it, and one read answers for all of it, so a play over the cluster's own inventory would otherwise do the work once per host. Set `no_log: true` on any task carrying a credential: a binary module has no per-parameter `no_log`, so the task-level setting is what suppresses the arguments and the result.

Credentials given as parameters are rendered as flags, which puts them in the process table for the life of the run. `zarf_config` names a zarf configuration file instead, which zarf reads them from.

## What the result says

The two converging modules report `changed: true` with `changedSignal: partial` on every successful run, and name every component they deployed in `changedUndeclared`. Zarf reports which components ran and not whether any of them found the cluster already as it wanted, so `partial` is the honest answer rather than an unfinished one. Check mode reports the task as skipped, because neither command has a dry run.

The three read-only modules report `changed: false`, which is not an approximation, and they run in check mode rather than skipping: answering a question during a run an operator asked to be told about is the honest thing to do.

`zarf_state_info` withholds what it reads. The `zarf-state` Secret carries the registry, git and artifact server credentials and the agent webhook's TLS private key, so the returned `state` has them removed and `redacted` names the paths that went. `include_credentials: true` returns them and censors the task's own output in the same breath; the task that reads the result afterwards needs its own `no_log`, because `set_fact` prints what the module hid.

The [module guide](../../guides/zarf-ansible-module.md) covers installing the modules, the ordering a cluster with no storage provider needs, the live progress display, and the shape of the result.
