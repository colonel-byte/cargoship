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

## `zarf_state_info` withholds the credentials it reads

The `zarf-state` Secret is not state with some credentials in it. As of zarf v0.85.0 it holds eight: `registryInfo.pushPassword`, `registryInfo.pullPassword`, `registryInfo.secret`, `gitServer.pushPassword`, `gitServer.pullPassword`, `artifactServer.pushPassword`, `agentInfo.tls.key`, and the deprecated `agentTLS.key` that duplicates it. The last one is the agent webhook's TLS private key, which is a different severity from a registry password: a password is scoped to a registry, and that key is the admission webhook's identity.

So the module redacts by default. `include_credentials` is false unless set, the secret leaves are removed from `state`, and the dotted paths removed come back in `redacted`. An operator who needs a password sees `redacted: ["registryInfo.pushPassword", ...]` and knows which parameter to set, which is a better discovery path than documentation.

Three decisions inside that are worth the words:

**The keys are removed, not blanked.** A missing key fails a template with `'dict object' has no attribute 'pushPassword'`, which names the problem. A sentinel like `VALUE_REDACTED` templates cleanly into a registry login and fails somewhere else entirely, hours later, as an authentication error against a registry whose password is right in the Secret.

**Matching is by leaf name, not by a list of the eight paths.** A path list is more precise and goes stale the next time zarf adds a field, and what staleness costs here is a leaked credential rather than a failed task. Leaf-name matching picks up a new field under a name like these without anybody revisiting the file. The cost is over-redaction: a future benign field named `*key` would be withheld. That is the right direction to be wrong in. `agentInfo.tls.ca` and `.cert` are deliberately not matched -- they are public certificates, and templating the CA into a trust bundle is a reason to call this module.

**A run that asks for the credentials censors its own output.** `executor/task_result.py` honours an `_ansible_no_log` key in a module's *returned* data, not just the task's own `no_log`, so the module sets it when `include_credentials` is true. The registered variable still holds the real state; only the display is suppressed. Asking for the data is not the same as asking for it to be printed, and relying on the playbook author to have remembered `no_log` is relying on the thing that fails.

### `set_fact` prints what the module hid

This is the part that is easy to get wrong, and it is why `zarf_no_log` defaults to true on *both* of the `state` role's tasks rather than just the module call. A `set_fact` that reads a censored result prints the value in its own result:

```
TASK [set_fact from a censored result] *****
ok: [localhost] => {
    "ansible_facts": {
        "zarf_state": "hunter2"
    },
```

That is at `-v`, with the producing task fully censored. `no_log` on the module alone looks correct in review and leaks on the next line.

One thing that is already safe, and worth not breaking: `set_fact` defaults `cacheable: false` (`plugins/action/set_fact.py`), so the fact stays in the play's memory and never reaches a configured fact cache plugin. A `cacheable: true` here would write the cluster's registry credentials into a jsonfile cache on the control node, outliving the play that read them. Do not add it.

### How it is tested

A redaction is a claim about the contents of a returned dictionary, so unlike the flag rendering above it cannot be read out of the source -- it has to run. `TestZarfStateInfoWithholdsCredentials` in `test/e2e/noncluster` drives a playbook that asserts the redacted paths by name, that no credential value survives anywhere in the document, that every non-credential field is intact, and that `include_credentials` returns the whole thing.

It needs no cluster. The module's one impure step is running zarf, so `testdata/zarf-state-stub.sh` stands in for it and prints the base64 of `testdata/zarf-state.json`, a copy of the shape zarf v0.85.0 writes with every credential replaced by a `FIXTURE-` placeholder. That covers the decode, the redaction, the role, and the fact.

Two things about that test are deliberate rather than incidental:

- **It counts the assertions that reported passing.** A renamed fact or a skipped block leaves every remaining assertion passing, and `failed=0` would call that a success. The expected count is read out of the playbook rather than written in the Go file, so adding an assertion does not quietly stop the count from meaning anything.
- **It reads the playbook, the fixture and the plugin itself before running anything.** Go's test cache keys on the files the test process opens, and everything this test exercises is opened by the `ansible-playbook` child. Without those reads, editing the plugin leaves a passing result cached -- which is precisely the shape of mistake that gets a credential leak marked as fixed. This was confirmed by sabotaging the redaction: before the reads were added the cached pass survived, and after them the test fails as it should.

## One thing that did not generalise

`--no-color` is rendered by `zarf_package_info` and `zarf_package_inspect`, which parse zarf's stdout as JSON and YAML respectively and cannot have colour codes in it. It is not rendered by `zarf_state_info`, and not for want of tidiness: `zarf tools kubectl` hands its arguments to kubectl, which answers `unknown flag: --no-color` and exits. It is in each `command_parts` rather than in the shared base class for that reason, with the reason written at the point where its absence looks like an oversight.
