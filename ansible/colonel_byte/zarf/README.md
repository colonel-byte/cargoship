# colonel_byte.zarf

Initialise an air-gapped cluster with [zarf](https://zarf.dev) from an Ansible playbook. This collection is a proof of [ZEP-0072](https://github.com/zarf-dev/proposals/pull/73), built in the cargoship repository because the pattern it proposes already runs there as `colonel_byte.cargoship`.

## Architecture

Zarf is not an SSH orchestrator. It talks to one cluster through a kubeconfig, from the node it runs on, so this collection runs `zarf` on the node the task is delegated to and nothing on any managed node. There is no inventory to project and no fleet to connect to: what a task supplies is a kubeconfig, a package, and the parameters the zarf command takes.

Each module is a compiled wrapper binary rather than a Python module:

| Layer                                             | What it is                                                                                                                                                         |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `colonel_byte.zarf.zarf_init`                     | A wrapper binary built from `cmd/zarf/init`. It renders the `zarf init` command line, runs the installed zarf, follows the deployment, and writes one JSON object. |
| `colonel_byte.zarf.zarf_package_deploy`           | The same, for `zarf package deploy`, built from `cmd/zarf/deploy`.                                                                                                 |
| `plugins/action/zarf_init.py`, `..._deploy.py`    | The documented parameter surfaces, and the delivery and display described below. They hold no translation rules.                                                   |
| `plugins/plugin_utils/projection.py`              | Argument delivery, the heartbeat monitor thread, and result parsing. The whole of the Python in this collection.                                                   |

ZEP-0072 proposes putting the module dispatch inside the `zarf` binary itself, which a proof in this repository cannot do. The wrappers are the one deliberate divergence; everything else -- `argv[0]` dispatch, the WANT_JSON intake, the stdout guard, the single JSON result, the heartbeat side channel -- is the shape the proposal describes.

## Installing

Build the wrappers and put them on the path of the node the play runs on:

```sh
go build -o zarf_init ./cmd/zarf/init
go build -o zarf_package_deploy ./cmd/zarf/deploy
sudo install -m 0755 zarf_init zarf_package_deploy /usr/local/bin/
```

Then make the collection visible to Ansible, by copying or symlinking this directory to `~/.ansible/collections/ansible_collections/colonel_byte/zarf`.

## Using it

```yaml
- name: Initialise the cluster
  hosts: localhost
  connection: local
  tasks:
    - name: Run zarf init
      colonel_byte.zarf.zarf_init:
        init_package: /srv/staging/zarf-init-amd64-v0.85.0.tar.zst
        kubeconfig: /etc/rancher/rke2/rke2.yaml
        registry_mode: proxy
        timeout: 15m
      delegate_to: localhost
      run_once: true
      no_log: true
      register: init_result
```

A play running with `-v` shows each component as zarf reaches it:

```
[zarf] component 1: zarf-injector [running]
[zarf] component 1: zarf-injector [done]
[zarf] component 2: zarf-seed-registry [running]
```

## What the result holds

```json
{
  "changed": true,
  "msg": "initialised the cluster",
  "zarf": {
    "module": "init",
    "command": ["zarf", "init", "--confirm", "--no-color", "--log-format", "json"],
    "directory": "/srv/staging",
    "componentsRan": ["zarf-injector", "zarf-seed-registry", "zarf-registry"],
    "changedSignal": "partial",
    "changedUndeclared": ["zarf-injector", "zarf-seed-registry", "zarf-registry"]
  }
}
```

`changedSignal` is `partial` for every successful run, and that is honest rather than unfinished. Zarf reports which components it deployed and does not report whether a component found the cluster already in the state it wanted, so the wrapper reports `changed: true` and names every component as undeclared. A module inside zarf, with access to the deployer, is what could report `complete`.

Credential values are replaced in the reported command line. They are still rendered as flags for the run itself, which puts them in the process table: name a zarf config file with `zarf_config` to keep them out of it.

## Two modules, because one is not enough

A cluster whose distribution ships no storage provider cannot be initialised on its own: zarf's registry claims a PVC during the seed phase, and with no `StorageClass` the claim stays `Pending` until the Helm install times out. A storage provider package deployed with `connected: true` needs no registry, and so needs no initialised cluster, which makes it the one thing that can come first.

That alternation -- provider, init, provider, init -- is the shape of the [`init-local-path` uds-bundle](https://github.com/colonel-byte/uds-bundles/blob/main/upstream/init-local-path/uds-bundle.yaml), and `test/e2e/zarf` runs it as a playbook against two real k3d clusters, one with k3s's StorageClass and one without. See the guide at [zarf-ansible-module](https://colonel-byte.github.io/cargoship/guides/zarf-ansible-module.html).

## Reference documentation

The per-module reference pages are generated from the `DOCUMENTATION` and `EXAMPLES` blocks in the action plugins by `mage generate:document`, and land under `docs/ansible/zarf/` -- published as the [collection reference](https://colonel-byte.github.io/cargoship/ansible/zarf/collection.html). `go test ./internal/zarfmod -run TestActionPluginDocsMatchModuleParams` is what holds those blocks against the Go parameter structs, so a parameter added to one and not the other fails a test rather than a playbook.

## Known limits of the proof

- Check mode reports the task as skipped. Neither `zarf init` nor `zarf package deploy` has a dry run.
- The collection is a proof of concept and is expected to be removed from this repository once ZEP-0072 is resolved. See [choice-zarf-collection-removal](../../../docs/agent/choice-zarf-collection-removal.md).
- Component progress is read back out of zarf's log stream, so it depends on zarf's log messages rather than on an API. A renamed log message costs the progress display and nothing else.
- The flags the wrappers render are held against recorded lists in `internal/zarfmod/testdata/zarf-init-flags.txt` and `zarf-package-deploy-flags.txt`, not against zarf's own command tree. Refresh them when the zarf they run moves.
- The monitor reads one status file every 500ms, so a component that finishes and is replaced inside that window shows its `[running]` line and not its `[done]` line. Real init components take minutes; a fake zarf that finishes one per second does not.
