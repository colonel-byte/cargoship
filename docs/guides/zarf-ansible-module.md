# Driving zarf from Ansible

The `colonel_byte.zarf` collection initialises a cluster with zarf and deploys zarf packages onto it from a playbook. It is a proof of [ZEP-0072](https://github.com/zarf-dev/proposals/pull/73), which proposes native Ansible module support for zarf, built here because the pattern it proposes is the one `colonel_byte.cargoship` already runs. See [ansible-module](ansible-module.md) for that collection, whose shape this one copies.

This guide covers what the two modules are, how to install them, the two-module walk that a cluster with no storage provider needs, and what the result holds. The full parameter set of each module, and the zarf flag each parameter renders, is on its reference page: [`zarf_init`](../ansible/zarf/module_init.md) and [`zarf_package_deploy`](../ansible/zarf/module_package_deploy.md), both generated from the collection. For why the modules are separate wrapper binaries rather than the `zarf` binary itself, see [choice-zarf-ansible-module](../agent/choice-zarf-ansible-module.md).

## The two modules

| Module                                  | Command it renders   | What it is for                                                                       |
| --------------------------------------- | -------------------- | ------------------------------------------------------------------------------------ |
| `colonel_byte.zarf.zarf_init`           | `zarf init`          | Initialise a cluster: the injector, the seed registry, the registry, the agent       |
| `colonel_byte.zarf.zarf_package_deploy` | `zarf package deploy`| Put a package on a cluster, from a path, an `oci://` reference, or an `https://` URL |

Both run on the node the task is delegated to, not on the cluster's nodes. Zarf reaches the cluster through a kubeconfig, so there is no inventory to project and nothing to copy to a managed node: a task supplies a kubeconfig, a package, and the parameters the zarf command takes.

Neither module is a Python module. Each is a compiled wrapper binary that renders the zarf command line from the task's parameters, runs the **installed** zarf, follows the deployment component by component, and writes one JSON object back. The zarf that does the work is the one on the node's path, which is what keeps the module and the tool in step.

## Installing

The cargoship `.rpm`, `.deb`, and `.apk` install the collection to `/usr/share/ansible/collections/ansible_collections/colonel_byte/zarf/` -- on Ansible's default collections path -- along with `/usr/bin/zarf_init` and `/usr/bin/zarf_package_deploy` and the module symlinks that point at them. Installing the package is all a management node needs, beyond a zarf of its own.

From a checkout, build the module files and put them on the path:

```sh
go run ./magefiles/core test:endToEndZarf   # builds both, then runs the e2e suite
# or just the binaries:
go build -o build/zarf_init ./cmd/zarf/init
go build -o build/zarf_package_deploy ./cmd/zarf/deploy
```

The `cargoship-ansible` container image carries all of it already -- `ansible-core`, both collections, both module files, and a `zarf` binary copied from zarf's own signed agent image -- so a run from that image needs nothing installed. See [ansible-container](ansible-container.md).

A task can name a wrapper outright rather than relying on the path, which is what the e2e suite does:

```yaml
- name: Initialise the cluster
  colonel_byte.zarf.zarf_init:
    wrapper_binary: /home/operator/cargoship/build/zarf_init
    init_package: /srv/staging/zarf-init-amd64-v0.85.0.tar.zst
    kubeconfig: /etc/rancher/rke2/rke2.yaml
  delegate_to: localhost
  run_once: true
  no_log: true
```

Set `run_once: true` and `delegate_to` on every task. One `zarf init` initialises the whole cluster, so a play over the cluster's own inventory would otherwise initialise it once per host. Set `no_log: true` on any task carrying a credential: a binary module has no per-parameter `no_log`, so the task-level setting is what suppresses the arguments and the result.

## Naming the init package

`init_package` is the package zarf deploys, passed as the positional package source `zarf init [ PACKAGE_SOURCE ]` takes. A path to a staged tarball under any name, an `oci://` reference, or an `https://` URL all work, and so does a directory -- which leaves zarf to search inside it the way it does when given no source at all, for a file named after zarf's own version.

```sh
zarf package pull oci://ghcr.io/zarf-dev/packages/init:v0.85.0 --architecture amd64 \
  --output-directory /srv/staging
```

A package pulled that way keeps the `zarf-init-<arch>-<version>.tar.zst` name, and the version has to be the version of the zarf that will deploy it: `zarf version` on the node that runs the play is what picks the tag, including inside the `cargoship-ansible` image, whose zarf is a pinned release of its own. A package built in-house is under no such constraint about its name, because nothing looks it up by name.

This needs zarf **v0.72.0 or newer**, which is where `zarf init` gained that positional argument. Before it, the only way to choose a package was to run zarf in the directory holding a file named after zarf's own version, and the module did exactly that -- so an older zarf rejects the argument as a usage error naming neither the parameter nor the version that would accept it. The wrapper reads `zarf version` before it runs and fails with both versions instead, and reports what it found as `zarf.version` either way.

`directory` is now a separate thing: the working directory zarf runs in, which is where a relative `init_package` and a relative path inside a `zarf_config` file resolve from. The two may be set together.

```yaml
- name: Initialise the cluster from a package built in-house
  colonel_byte.zarf.zarf_init:
    init_package: /srv/staging/our-init-package.tar.zst
    kubeconfig: /etc/rancher/rke2/rke2.yaml
  delegate_to: localhost
  run_once: true
  no_log: true

- name: Initialise the cluster from a package held in an internal registry
  colonel_byte.zarf.zarf_init:
    init_package: oci://registry.bubbles.test/zarf/init:v0.85.0
    kubeconfig: /etc/rancher/rke2/rke2.yaml
  delegate_to: localhost
  run_once: true
  no_log: true
```

## The walk a cluster with no storage needs

A cluster whose distribution ships no storage provider cannot be initialised in one task, and the reason is worth knowing before a playbook is written: zarf's registry claims a 20Gi `PersistentVolumeClaim` during the seed phase. With no `StorageClass` to satisfy it the claim stays `Pending` and the Helm install times out. Disabling the claim does not help -- the registry chart then renders no storage configuration at all and the container exits.

What does work is deploying a storage provider first, with `connected: true`. A connected deploy does not push the package's images, so it needs no registry, and so needs no initialised cluster. That is the one zarf operation available on a cluster nothing has touched, and it is why this collection has a deploy module as well as an init one:

```yaml
- name: Deploy the storage provider onto the uninitialised cluster
  colonel_byte.zarf.zarf_package_deploy:
    package: oci://ghcr.io/colonel-byte/zarf/csi-local-path-provider:0.0.37-upstream
    components: local-path-images,local-path-chart
    kubeconfig: "{{ zarf_kubeconfig }}"
    public_key: /etc/zarf/colonel-byte-zarf-packages.pub
    verify: always
    connected: true
  delegate_to: localhost
  run_once: true

- name: Initialise the cluster
  colonel_byte.zarf.zarf_init:
    init_package: /srv/staging/zarf-init-amd64-v0.85.0.tar.zst
    kubeconfig: "{{ zarf_kubeconfig }}"
    registry_mode: proxy
  delegate_to: localhost
  run_once: true
```

This is the shape the [`init-local-path` uds-bundle](https://github.com/colonel-byte/uds-bundles/blob/main/upstream/init-local-path/uds-bundle.yaml) carries, which lists the init package and the storage provider twice each and alternates between them. `test/e2e/zarf/testdata/provider-first.yml` and `bundle-order.yml` are that bundle written as playbooks, and both are run by `mage test:endToEndZarf`.

Two parameters earn their keep on the second pass over an already-provisioned cluster:

- `take_ownership: true` lets zarf adopt objects it did not create. A cluster that already has a `local-path` `StorageClass` -- k3s ships one -- makes Helm refuse the provider package without it.
- `force_conflicts: true` lets server-side apply overwrite fields another controller owns, which is the next thing that refuses after ownership is settled.

### Registry proxy mode

`registry_mode: proxy` is worth preferring on a small or local cluster. Zarf's default nodeport mode publishes the registry on a Kubernetes NodePort and pushes every image through it; on a k3d cluster that push is slow enough to time out and be retried, which adds minutes to an initialisation. Proxy mode puts a DaemonSet on a host port instead. The mode zarf settled on is recorded in `registryInfo.registryMode` inside the `zarf-state` secret.

## What the result holds

```json
{
  "changed": true,
  "msg": "initialised the cluster",
  "zarf": {
    "module": "init",
    "command": ["zarf", "init", "--confirm", "--no-color", "--log-format", "json", "--registry-mode", "proxy"],
    "directory": "/srv/staging",
    "componentsRan": ["zarf-injector", "zarf-seed-registry", "zarf-registry", "zarf-agent"],
    "changedSignal": "partial",
    "changedUndeclared": ["zarf-injector", "zarf-seed-registry", "zarf-registry", "zarf-agent"],
    "statusFile": "/tmp/zarf-status-m56nictp.json"
  }
}
```

`componentsRan` is what a playbook has in place of per-component tasks, and `command` is the command line to reproduce the run by hand -- with credential values replaced, so it is safe to print.

`changedSignal` is `partial` for every successful run, and that is honest rather than unfinished. Zarf reports which components it deployed and does not report whether any of them found the cluster already as it wanted, so the modules report `changed: true` and name every component as undeclared. A module **inside** zarf, with access to the deployer, is what could report `complete`.

### Live progress

Zarf deployments take minutes, and an Ansible task prints nothing until it returns. The wrapper writes each component boundary to a status file and the action plugin displays it, so a play run with `-v` reports progress as it happens:

```console
[zarf] component 1/4: zarf-injector [running]
[zarf] component 2/4: zarf-seed-registry [running]
[zarf] component 3/4: zarf-registry [running]
```

The count on the left is only known when the task names `components`; otherwise the display reports `component 3` rather than `component 3/4`, which is what an honest unknown looks like. The monitor polls twice a second, so a component that finishes and is replaced inside that window shows its `[running]` line and not its `[done]` line.

## Check mode and failures

Check mode reports the task as **skipped**. Neither `zarf init` nor `zarf package deploy` has a dry run, and running anyway during a `--check` play is the one thing check mode must not do.

A failure is reported inside the result, with the process still exiting 0, so Ansible shows the module's message rather than its own `MODULE FAILURE` diagnosis. Zarf's own output goes to stderr, where Ansible captures it as `module_stderr`: standard output carries the one JSON object and nothing else.

```yaml
- name: Deploy a package
  colonel_byte.zarf.zarf_package_deploy:
    package: /srv/staging/zarf-package-monitoring-amd64.tar.zst
    kubeconfig: "{{ zarf_kubeconfig }}"
  register: deploy
  failed_when: deploy.failed
```

## Credentials

Parameters carrying credentials -- the registry and git push and pull passwords, the registry secret -- are rendered as flags, which puts them in the process table for the life of the run. Keep them out of it by putting them in a zarf config file and naming it with `zarf_config`, which the module passes as `ZARF_CONFIG`:

```yaml
- name: Initialise with credentials zarf reads from its own config file
  colonel_byte.zarf.zarf_init:
    init_package: /srv/staging/zarf-init-amd64-v0.85.0.tar.zst
    kubeconfig: "{{ zarf_kubeconfig }}"
    zarf_config: /etc/zarf/zarf-config.yaml
  delegate_to: localhost
  run_once: true
  no_log: true
```

The parameters themselves never reach disk on the path this collection's action plugins take: they are piped to the wrapper on stdin. A connection that cannot carry data on stdin -- SSH without pipelining -- falls back to a `0600` arguments file in a `0700` directory, removed when the task ends. Both are conventions ZEP-0072 weighs; the wrapper reads either.

## Reproducing a module run by hand

Both wrappers answer as the module named by the basename they were invoked under, or by `ZARF_ANSIBLE_MODULE`, and read their parameters from a JSON file named as the single argument or from stdin:

```sh
printf '%s' '{"init_package":"/srv/staging/zarf-init-amd64-v0.85.0.tar.zst","kubeconfig":"/etc/rancher/rke2/rke2.yaml"}' \
  | ZARF_ANSIBLE_MODULE=init zarf_init
```

This is the invocation the action plugin makes, so it is the one to reach for when a task fails and the question is whether the module or the playbook is wrong.

## Testing

`mage test:endToEndZarf` builds both wrappers and runs the e2e suite, which stands up two k3d clusters -- one with a storage provider and one without -- and walks the uds-bundle's four packages across each. It needs `k3d`, `zarf`, `ansible-playbook`, Docker, and the network to pull the packages once; each test skips with a reason when one is missing. `mage test:cleanZarfClusters` removes the clusters a killed run left behind.
