# Why the read-only zarf modules are Python rather than wrapper binaries

Every other module in `colonel_byte.zarf` is a wrapper binary. `plugins/plugin_utils/projection.py` opens by saying its Python is "deliberately the whole of" the collection's Python, and [choice-zarf-ansible-module](choice-zarf-ansible-module.md) records why: the parameters are a Go struct in `internal/zarfmod`, the command line is rendered by a Go function, and `plugindocs_test.go` holds the Python `DOCUMENTATION` block against both.

`zarf_package_info`, `zarf_package_inspect`, and `zarf_state_info` are not built that way. Their whole implementation is the action plugin, sharing `ZarfInfoActionBase` in `plugins/plugin_utils/info.py`. This is the record of that decision, and of what it costs.

## Why the wrapper buys nothing here

The wrapper exists for three reasons, and a read-only command has none of them.

**There is no flag surface worth projecting.** `zarf init` renders more than thirty flags, which is why spelling them out in Go and checking them against a recorded list earns its keep. These three render one flag between them that any parameter controls -- `--key` on `zarf_package_inspect`. The rest of each command line is fixed words.

**There is no progress to follow.** `internal/zarfmod/progress.go` reads component boundaries back out of zarf's log stream so that a fifteen-minute init is not a silent play. `zarf package list` returns in under a second and has no components. The heartbeat file, the status-file parameter, and the polling monitor thread in `ZarfActionBase` would all be carried for nothing.

**There is no `changed` to signal.** The hardest question in the wrapper -- what `changedSignal` can honestly be when zarf reports which components ran and not whether any of them had work to do -- does not arise. These modules always report `changed: false`, and that is not an approximation.

Against that, building them as wrappers would add three `main` packages under `cmd/zarf/`, three files in `internal/zarfmod`, three more binaries in every release, and three more things an operator has to install on the management node before a playbook runs. [choice-zarf-collection-removal](choice-zarf-collection-removal.md) has the whole collection scheduled for deletion once ZEP-0072 resolves, which is a poor thing to spend a release artifact on.

## What is checked instead

The gap this leaves is not that the modules are Python. It is that `plugindocs_test.go` is a table of modules, so a module absent from the table is a module no test looks at, and nothing says so. That is the state these three were first written in. Two tests in `internal/zarfmod/infoplugins_test.go` close it:

- `TestEveryActionPluginIsCovered` globs `plugins/action/*.py` and requires each plugin to appear in exactly one of two lists: `wrapperModuleCases`, the table the wrapper modules are checked through, or `infoModules`, the read-only ones. A plugin in neither fails the test by name. It also fails on a list naming a plugin that no longer exists, since that is a check that passes by checking nothing.
- `TestInfoPluginDocsMatchRenderedFlags` holds each read-only module's documented `cli_flag` values against the flags its `command_parts` function names, in both directions.

`command_parts` is a module-level function of the task's parameters, rather than a few lines inside `run()`, precisely so that the second test has something to read. The same technique holds the variable allowlist in the cargoship collection's projection plugin: see `internal/ansibleinv/plugin_test.go`.

## What that test cannot see

It reads string literals out of Python source, so it is weaker than its wrapper-side counterpart, which populates a struct and renders a real command line. It sees which flags are named. It does not see which parameter a flag is rendered for, in what order, or whether a value is attached with a space or an `=`. `--verify=always` -- the spelling [choice-zarf-ansible-module](choice-zarf-ansible-module.md) records as load-bearing, because the space form leaves `always` as a positional argument and zarf rejects the command -- is exactly the mistake it would miss. It also depends on every flag being a literal inside `command_parts`, which is why none is assembled from a variable.

That is an acceptable trade at one parameter-controlled flag per module. It stops being one well before the flag surface gets interesting, and the threshold is worth naming: a read-only module that grows a second or third flag, or any flag whose value attaches in a way pflag cares about, wants a wrapper binary rather than a better regular expression.

## One thing that did not generalise

`--no-color` is rendered by `zarf_package_info` and `zarf_package_inspect`, which parse zarf's stdout as JSON and YAML respectively and cannot have colour codes in it. It is not rendered by `zarf_state_info`, and not for want of tidiness: `zarf tools kubectl` hands its arguments to kubectl, which answers `unknown flag: --no-color` and exits. It is in each `command_parts` rather than in the shared base class for that reason, with the reason written at the point where its absence looks like an oversight.
