# Running Cargoship as an Ansible Module

Cargoship runs inside a playbook as an Ansible module. The cargoship binary *is* the module: it is installed as a set of symlinks named `cargoship_<action>`, and a symlink's name selects the action. There is no separate module package and nothing to install on the managed nodes.

The inventory a module takes is the one described in [Generating an Inventory from Ansible](ansible-inv.md) -- Ansible's resolved `groups` and `hostvars`, plus a `cluster` block -- and the group mapping and host variables documented there apply here unchanged. This guide covers the modules themselves: how the collection is installed, how a task is written, and what its result means. The parameter reference for each module and for the `cluster` role is generated from the collection and lives in [the collection reference](../ansible/collection.md).

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

## Parameters

`inventory` takes the document described in [Generating an Inventory from Ansible](ansible-inv.md), `groups` and `hostvars` included. Everything else maps onto a flag of the command the module runs.

The parameter reference lives with the modules rather than here, so that it cannot drift from what the binary accepts: [`cargoship_apply`](../ansible/module_apply.md), [`cargoship_prepare`](../ansible/module_prepare.md), [`cargoship_engine_config_sync`](../ansible/module_engine_config_sync.md), [`cargoship_reset`](../ansible/module_reset.md), and [`cargoship_kube_config`](../ansible/module_kube_config.md). Each page lists every parameter the module takes, its type, and the flag it renders. [Modules](../ansible/modules.md) is the index, and covers why the surfaces differ from one another.

Of the five, `cargoship_engine_config_sync` is the most Ansible-shaped thing cargoship does: it converges configuration across a fleet that is already installed.

A parameter the module does not know is an error naming it. A parameter left unset is left unset: the module passes no flag for it, so whatever your cargoship configuration file sets still applies. That is why `label_nodes: false` and omitting `label_nodes` are different.

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

Every variable the role takes, with its default, is on [role_cluster](../ansible/role_cluster.md). Anything the role does not name a variable for goes in `cargoship_args`, which is passed to the chosen module verbatim.

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
