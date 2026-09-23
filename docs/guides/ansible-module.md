# Running Cargoship as an Ansible Module

Cargoship runs inside a playbook as an Ansible module. The cargoship binary *is* the module: it is installed as a set of symlinks named `cargoship_<action>`, and a symlink's name selects the action. There is no separate module package and nothing to install on the managed nodes.

The inventory a module takes is the one described in [Generating an Inventory from Ansible](ansible-inv.md) -- Ansible's resolved `groups` and `hostvars`, plus a `cluster` block -- and the group mapping and host variables documented there apply here unchanged. This guide covers the modules themselves: how the collection is installed, what each module takes, and what its result means.

Ansible supplies the inventory and nothing else. It does not connect to the fleet, gather facts on it, or run a task per host. Every task runs on one management node outside the cluster -- the node the package and images were staged onto -- and cargoship opens every SSH connection itself from there. See [choice-ansible-module](../agent/choice-ansible-module.md) for why the work is split that way.

Five modules ship, one per fleet action: `cargoship_apply`, `cargoship_prepare`, `cargoship_engine_config_sync`, `cargoship_reset`, and `cargoship_kube_config`. Package creation is not among them -- it builds an artifact from a definition in a repository, which belongs in a pipeline rather than in a convergence run.

Working inventories and a playbook for each of the five are in [ansible/examples](https://github.com/colonel-byte/cargoship/tree/main/ansible/examples), along with a script that translates an inventory without running a play.

```yaml
- name: Converge engine configuration
  cargoship_engine_config_sync:
    package: /srv/staging/rke2-1.31.tar.zst
    inventory_path: /srv/staging/generated-inventory.yaml
    inventory:
      groups: "{{ groups }}"
      hostvars: "{{ hostvars }}"
      cluster:
        name: bubbles
        loadbalancer: bubbles-kc.test.com
    vault_password_file: /srv/staging/vault-pass
  delegate_to: localhost
  no_log: true
```

The task runs on the management node -- `connection: local` when the Ansible controller is that node, `delegate_to` when they are separate. Cargoship connects to the fleet itself, from there.

## Installing the collection

The modules live in the `colonel_byte.cargoship` collection. The RPM and deb packages install it to `/usr/share/ansible/collections`, which is on ansible-core's default collections path, so on a management node that installed cargoship from a package there is nothing further to do.

Installing from a tarball or a checkout instead, install the collection, then point its module files at the binary. Each release carries a built `colonel_byte-cargoship-<version>.tar.gz` and a cosign bundle beside it; the collection is not published to Galaxy, because the node that installs it has no route to it.

```sh
cosign verify-blob --key cosign.pub \
  --bundle colonel_byte-cargoship-0.25.0.tar.gz.sigstore.json \
  colonel_byte-cargoship-0.25.0.tar.gz
ansible-galaxy collection install colonel_byte-cargoship-0.25.0.tar.gz
./hack/ansible-link-modules.sh ~/.ansible/collections/ansible_collections/colonel_byte/cargoship /usr/local/bin/cargoship
```

From a checkout, build the tarball first with `hack/ansible-build-collection.sh ./bin/ansible`, which is the same script the release pipeline runs.

That last step is not optional and has no equivalent in Galaxy's own packaging. Ansible resolves a task name to a module *file*, so each action needs a file named after it; the files are symlinks to the one cargoship binary, which is what the packages create and what the script creates by hand. A collection installed without them fails with the module not being found.

The collection carries one action plugin per module. It runs inside `ansible-playbook`, on the interpreter Ansible is already running on, and does one thing: copy `groups` and `hostvars` into the module's `inventory` parameter so you do not have to. Nothing is installed on the fleet, and running cargoship by hand still needs no Python.

It also accepts a `parameters` dict and merges it into the rest, which is how the role passes a caller-supplied set of parameters without templating the whole of a task's arguments -- something Ansible warns about, and rightly. A parameter given both directly and inside `parameters` is an error naming it.

The plugin forwards the four `ansible_*` connection variables listed in [Generating an Inventory from Ansible](ansible-inv.md) and every `cargoship_*` variable, and drops the rest. A host's resolved variables routinely carry credentials for things that are not this cluster, and a module's parameters are written to a file on disk.

When defining `profiles` inside `cargoship_cluster` in YAML, note type constraints: `concurrency` (e.g. `"1"`, `"50%"`) and port definitions (`ports[].port`, e.g. `"6443"`) are unmarshaled as strings by the underlying schema. Quote numeric strings in YAML to prevent JSON numeric unmarshaling failures.

## Parameters every module takes

`inventory` takes the document described in [Generating an Inventory from Ansible](ansible-inv.md), `groups` and `hostvars` included. Everything else maps onto a flag of the command the module runs.

| Parameter        | Flag           | Notes                                                                       |
| ---------------- | -------------- | --------------------------------------------------------------------------- |
| `inventory`      |                | Required. The resolved Ansible inventory, plus `cluster`.                   |
| `inventory_path` | `--config`     | Where to write the generated document. A private temporary file when unset. |
| `log_level`      | `--log-level`  | Defaults to `debug` when the play runs with `-v`.                           |
| `log_format`     | `--log-format` |                                                                             |
| `log_file`       | `--log-file`   | Boolean. Always write full-verbosity debug log to a file on the host.       |
| `age_recipient`  |                | List of public keys to encrypt registry credentials written to disk.        |

A parameter the module does not know is an error naming it. A parameter left unset is left unset: the module passes no flag for it, so whatever your cargoship configuration file sets still applies. That is why `label_nodes: false` and omitting `label_nodes` are different.

The surfaces differ because the commands do. `cargoship prepare` reads no encrypted value, so it takes no `vault_password_file`; `cargoship reset` installs nothing, so it takes no package and no `values`. A parameter offered to the wrong module is an error naming it, rather than a flag quietly dropped.

## `cargoship_apply`

Installs or converges the whole cluster.

| Parameter               | Flag                                             |
| ----------------------- | ------------------------------------------------ |
| `package`               | the positional argument. Required.               |
| `concurrency`           | `--concurrency`                                  |
| `work_concurrency`      | `--work-concurrency`                             |
| `hosts`                 | `--hosts`                                        |
| `firewall`              | `--firewall`                                     |
| `fapolicyd`             | `--fapolicyd`                                    |
| `label_nodes`           | `--label-nodes`                                  |
| `allow_unmanaged_nodes` | `--allow-unmanaged-nodes`                        |
| `update_kubeconfig`     | `--update-kubeconfig`                            |
| `kubeconfig`            | `--kubeconfig`                                   |
| `values`                | `--values`. A list of files.                     |
| `timeout`               | `--timeout`                                      |
| `vault_password_file`   | `--vault-password-file`                          |
| `age_identity_file`     | `--age-identity-file`. A list of files.          |
| `public_key`            | `--key`                                          |
| `verify`                | `--verify`. `never`, `if-possible`, or `always`. |

## `cargoship_prepare`

Stages a package onto the fleet and readies the hosts. Takes no key material.

| Parameter | Flag |
| ------------------ | ---------------------------------- |
| `package`          | the positional argument. Required. |
| `concurrency`      | `--concurrency`                    |
| `work_concurrency` | `--work-concurrency`               |
| `hosts`            | `--hosts`                          |
| `firewall`         | `--firewall`                       |
| `fapolicyd`        | `--fapolicyd`                      |
| `values`           | `--values`. A list of files.       |
| `timeout`          | `--timeout`                        |
| `public_key`       | `--key`                            |
| `verify`           | `--verify`                         |

## `cargoship_engine_config_sync`

Converges engine configuration across the fleet, which is the most Ansible-shaped thing cargoship does. It does not update the hosts themselves, so it takes none of the three host switches.

| Parameter | Flag |
| --------------------- | --------------------------------------- |
| `package`             | the positional argument. Required.      |
| `concurrency`         | `--concurrency`                         |
| `work_concurrency`    | `--work-concurrency`                    |
| `label_nodes`         | `--label-nodes`                         |
| `update_kubeconfig`   | `--update-kubeconfig`                   |
| `kubeconfig`          | `--kubeconfig`                          |
| `values`              | `--values`. A list of files.            |
| `timeout`             | `--timeout`                             |
| `vault_password_file` | `--vault-password-file`                 |
| `age_identity_file`   | `--age-identity-file`. A list of files. |
| `public_key`          | `--key`                                 |
| `verify`              | `--verify`                              |

## `cargoship_reset`

Removes the cluster from the fleet. There is no package: the distro to remove is named directly.

| Parameter          | Flag                         |
| ------------------ | ---------------------------- |
| `distro`           | `--distro`. `k3s` or `rke2`. |
| `concurrency`      | `--concurrency`              |
| `work_concurrency` | `--work-concurrency`         |
| `hosts`            | `--hosts`                    |
| `firewall`         | `--firewall`                 |
| `fapolicyd`        | `--fapolicyd`                |

## `cargoship_kube_config`

Fetches the cluster kubeconfig onto the management node. It is the one module that changes the node the play runs on rather than the fleet.

| Parameter    | Flag                         |
| ------------ | ---------------------------- |
| `distro`     | `--distro`. `k3s` or `rke2`. |
| `kubeconfig` | `--kubeconfig`               |

## Signature verification

`public_key` and `verify` are the package signature parameters, and they are the key-based ones only. Keyless verification identifies a signer against a Fulcio root and a transparency log, which needs network access a management node inside an airlock does not have. Verify a keyless-signed package with `cargoship package verify` before the play, where the flags and their exclusions are all available.

## Key material

Key material is always named by a path, never given by value. Ansible writes a module's parameters into a file on the managed node, and a password passed by value would be a password written to a file you did not choose the mode of. Set `no_log: true` on the task regardless: a binary module has no per-parameter `no_log`, so the task-level setting is what suppresses the arguments and the result.

## The `cluster` role

The role is the shorter way to write the same task. It picks the module from `cargoship_action`, assembles the `inventory` parameter, and sets `run_once`, `delegate_to`, and `no_log` for you.

```yaml
- name: Install the cluster
  ansible.builtin.include_role:
    name: colonel_byte.cargoship.cluster
  vars:
    cargoship_action: apply
    cargoship_package: /srv/staging/rke2-1.31.tar.zst
    cargoship_cluster:
      name: bubbles
      loadbalancer: bubbles-kc.test.com
    cargoship_args:
      vault_password_file: /srv/staging/vault-pass
      timeout: 45m
```

| Variable                       | Default     | Meaning                                                                      |
| ------------------------------ | ----------- | ---------------------------------------------------------------------------- |
| `cargoship_action`             | `apply`     | Which module to run. One of the five.                                        |
| `cargoship_package`            | unset       | The package. Required for `apply`, `prepare`, and `engine_config_sync`.      |
| `cargoship_cluster`            | `{}`        | The `cluster` block: name, load balancer, profiles, registries, values.      |
| `cargoship_role_groups`        | `{}`        | The group mapping, when your groups are not named `controller` and `worker`. |
| `cargoship_inventory_path`     | unset       | Where to write the generated document.                                       |
| `cargoship_age_recipients`     | `[]`        | Public keys to encrypt registry credentials on disk in generated inventory.  |
| `cargoship_age_identity_files` | `[]`        | Identity files/SSH private keys to decrypt registry credentials at apply.    |
| `cargoship_log_file`           | `false`     | Always write full-verbosity debug log to a file on the host (`--log-file`).  |
| `cargoship_args`               | `{}`        | Everything else, passed to the module as-is.                                 |
| `cargoship_delegate_to`        | `localhost` | The management node.                                                         |
| `cargoship_no_log`             | `true`      | Suppress task parameter logging. Set false for local debugging.              |
| `cargoship_show_result`        | `false`     | Print `cargoship_result.cargoship` after the run.                            |

Every task in the role carries `run_once: true`. Cargoship converges the whole fleet in one run, so a play over the fleet's own inventory would otherwise run a full convergence once per host. It defaults `cargoship_no_log: true` to protect decrypted credentials and fleet inventories from terminal and CI logs, which is why `cargoship_show_result` exists: the debug task prints cargoship's sanitized report without exposing secrets.

## Check mode and changed

`--check` runs cargoship's dry run. Phases opt into it one at a time -- a phase that has not said how it behaves under a dry run is reported and not run -- so a check-mode task connects, detects, gathers facts, and validates the hosts, and reports the rest as phases it would have run.

Under check mode, `changed` means what Ansible means by it there: a real run would change something. It is true when a phase that knows how to tell had work outstanding -- a fleet whose engine configuration has drifted -- and false when every such phase found nothing to do.

`cargoship_kube_config` is the exception. `cargoship kube-config` has no dry run, so under `--check` the module reports `skipped: true` and runs nothing at all. That is what Ansible does with a module that has declared it does not support check mode; a binary module has nowhere to declare it, so it says so in its result instead.

## What `changed` is worth

`changed` is built from what the phases themselves report, and a phase has to opt in. `cargoship.changedSignal` says how much of the run is covered:

| Signal     | Meaning                                                                                                    |
| ---------- | ---------------------------------------------------------------------------------------------------------- |
| `complete` | Every phase the run reached said whether it changed anything.                                              |
| `partial`  | Some did not. They are named in `cargoship.changedUndeclared`.                                             |
| `unknown`  | No phase reported at all, so `changed` is a convention rather than an observation, and is reported `true`. |

Today the engine configuration sync phases report and the rest do not, so an ordinary run reads `partial`. That is deliberate: a phase that has said nothing cannot make `changed` true, and it is not treated as having changed nothing either. `KubeConfig` and `LabelNodes` are the two in the undeclared list that can genuinely change something -- the local kubeconfig and the node role labels -- so if a handler of yours depends on either, gate on the result rather than on `changed`.

`cargoship.phasesRan` and `cargoship.phasesPlanned` name the phases the run executed and, under check mode, the ones it reported instead of running. Ansible sees one result for the whole fleet, so these stand in for the per-host detail a task-per-host module would give you.

## The result

```json
{
  "changed": true,
  "msg": "synchronised engine configuration across the fleet",
  "cargoship": {
    "module": "engine_config_sync",
    "inventoryPath": "/srv/staging/generated-inventory.yaml",
    "inventoryKept": true,
    "checkMode": false,
    "command": ["cargoship", "engine-config-sync", "..."],
    "phasesRan": ["Connect", "Detect OS", "Sync Registry Config Controller"],
    "changedSignal": "partial",
    "changedUndeclared": ["Connect", "Detect OS"]
  }
}
```

`command` is the command line the module ran, so a failure can be reproduced by hand on the management node. `inventoryPath` is the generated document. A run that fails leaves it on disk whether or not you named the path, because it is the first thing to look at when a failure reads like the wrong cluster; a run that succeeds removes it unless the path was yours.

Cargoship's own logging goes to stderr, which Ansible captures and shows on failure. Per-host detail lives there, not in the result: Ansible sees one task for the whole fleet, not one result per host.

## Live progress

A fleet-wide `apply` is one Ansible task that runs for several minutes, so the modules report where they are while they run. The action plugin prints one line per phase transition:

```
[cargoship] Phase 1/12: Connect to hosts [running]
[cargoship] Phase 1/12: Connect to hosts [done]
[cargoship] Phase 2/12: Detect host operating systems [running]
```

The counter is the phase's position in the list for that module, so `docs/phases/apply.md` and its siblings read as the same sequence. A phase that fails is printed `[failed]`, ahead of the failure Ansible itself reports.

None of this needs configuring. The action plugin creates a temporary status file, points the module at it, polls it while the module runs, and removes it afterwards whether the run succeeded or not.

Driving the binary directly gets the same progress through the same mechanism: set `CARGOSHIP_STATUS_FILE` to a path and cargoship rewrites that file as each phase starts and finishes, holding one JSON object -- `phase`, `index`, `total`, `status` (`running`, `completed`, or `failed`), `timestamp`, and `error` on a failure. The file is replaced atomically on every transition, so a reader either sees the previous phase or the current one, never a half-written object. It is removed when the run finishes. Leave the variable unset and cargoship writes nothing.
