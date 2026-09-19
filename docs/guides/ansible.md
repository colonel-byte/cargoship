# Generating an Inventory from Ansible

Cargoship can build its cluster inventory from an Ansible inventory you already have, deriving each host's role from the Ansible groups it belongs to. This guide covers that translation: the group mapping, the host variables cargoship reads, and the rules the translation follows.

Ansible supplies the inventory and nothing else. It does not connect to the fleet, gather facts on it, or run a task per host. Cargoship opens every SSH connection itself, from the node it runs on -- a management node outside the cluster, which is the node the package and images were staged onto. See [choice-ansible-module](../agent/choice-ansible-module.md) for why the work is split that way.

## What the translation reads

Cargoship does not parse an Ansible inventory file. Resolving an inventory means group membership, `group_vars`, `host_vars`, dynamic inventory plugins, and the precedence rules over all of them; Ansible does that and hands over the merged result. What cargoship reads is a JSON document holding that result:

```json
{
  "groups": {
    "all": ["kc01", "kc02", "kw01", "db01"],
    "controller": ["kc01", "kc02"],
    "worker": ["kw01"],
    "databases": ["db01"]
  },
  "hostvars": {
    "kc01": {"ansible_host": "10.1.2.3", "cargoship_hostname": "distro-kc01"},
    "kc02": {"ansible_host": "10.1.2.4", "cargoship_hostname": "distro-kc02"},
    "kw01": {"ansible_host": "10.1.2.5", "cargoship_hostname": "distro-kw01"},
    "db01": {"ansible_host": "10.1.2.9"}
  },
  "cluster": {
    "name": "bubbles",
    "loadbalancer": "bubbles-kc.test.com"
  }
}
```

`groups` and `hostvars` are Ansible's own, verbatim. `cluster` holds the settings that belong to the cluster rather than to any host, which an Ansible inventory has no way to carry: the cluster name, the load balancer address, and optionally `profiles`, `registries`, and `values`. Those are the same fields `.spec.config` takes in a hand-written inventory -- see the [inventory guide](setup-inv.md).

Translate it:

```sh
cargoship inventory from-ansible ./resolved.json -o ./inventory.yaml
cargoship validate ./inventory.yaml
```

The command reads stdin when given no file, and writes to stdout when given no `--output`, so it composes. `--name` and `--loadbalancer` override what the document says, which is how one projection of a fleet becomes more than one cluster without being rewritten.

## Roles come from groups

By default the Ansible group named `controller` supplies the control-plane nodes, and the group named `worker` supplies the rest. Those are cargoship's own role names, so the default guesses at no Ansible convention.

Set `roleGroups` to map cargoship's two roles onto the group names your inventory actually uses:

```json
{
  "roleGroups": {
    "controller": ["k8s_masters"],
    "worker": ["k8s_workers", "k8s_infra"]
  }
}
```

A role may take more than one group. Only `controller` and `worker` exist; naming any other role is an error.

Three rules govern what comes out:

- **A host in none of the mapped groups is left out.** The `db01` host in the example above is not part of the cluster and does not appear in the generated inventory. A play's inventory legitimately carries hosts that have nothing to do with Kubernetes.
- **A host in groups mapped to two different roles is an error**, naming the host and both groups. There is no precedence rule, because there is no defensible one: picking a role silently would install a cluster nobody asked for.
- **Controllers are written first, and the first controller becomes the cluster leader.** Within a role, hosts come out in the order the mapping names the groups, and within a group, in the order the inventory lists the hosts. If it matters which node bootstraps the cluster, put it first in the first controller group.

If `roleGroups` names a group the inventory does not define, that is an error -- a misspelled group name there has no other symptom. The default mapping is not held to that, so a cluster of nothing but control-plane nodes needs no `worker` group.

## Host variables

Connection details come from the Ansible variables you already set. Everything cargoship needs beyond those comes from variables under a `cargoship_` prefix.

| Inventory field | Variable | Default |
| --- | --- | --- |
| `ssh.address` | `ansible_host` | the inventory hostname |
| `ssh.user` | `ansible_user` | `root` |
| `ssh.port` | `ansible_port` | `22` |
| `ssh.keyPath` | `ansible_ssh_private_key_file` | unset |
| `ssh.hostKey` | `cargoship_host_key` | unset |
| `ssh.bastion` | `cargoship_bastion` | unset |
| `hostname` | `cargoship_hostname` | the inventory hostname |
| `profile` | `cargoship_profile` | the host's role |
| `privateAddress` | `cargoship_private_address` | unset |
| `privateInterface` | `cargoship_private_interface` | unset |
| `engine.labels` | `cargoship_node_labels` | unset |
| `engine.taints` | `cargoship_node_taints` | unset |
| `host` | `cargoship_host` | unset |
| `environment` | `cargoship_environment` | unset |
| `files` | `cargoship_files` | unset |

`ansible_host` and `cargoship_hostname` are different facts and both are kept: the first is where cargoship connects, the second is what the node calls itself. When neither is set, the name the inventory knows the host by serves as both.

The structured variables -- `cargoship_host`, `cargoship_files`, `cargoship_bastion`, `cargoship_node_labels`, `cargoship_node_taints`, `cargoship_environment` -- take exactly the shape the inventory schema documents for the field they configure, so there is one vocabulary to learn rather than two:

```yaml
cargoship_profile: control
cargoship_node_labels:
  adrp.xyz/purpose-control: "true"
cargoship_host:
  ports:
    - port: "6443"
      protocol: tcp
  firewall:
    rules:
      - name: allow-metrics
        action: allow
        source: 10.0.0.0/8
        port: "9100"
        protocol: tcp
cargoship_bastion:
  address: 10.0.0.1
  user: jump
  port: 22
```

A bastion is declared as its own variable rather than read out of `ansible_ssh_common_args`. Cargoship does not parse SSH argument strings, and an inventory that reaches its fleet through a jump host via `ansible_ssh_common_args` has to say so again here.

### Misspelled variables are errors

A variable under the `cargoship_` prefix that cargoship does not read is an error, and so is an unknown key inside one of the structured variables. Ansible has no notion of a variable belonging to anyone, so a misspelled `cargoship_profil` would otherwise sit in `hostvars` looking set and doing nothing, and a misspelled key would be dropped on the way in. A misspelled variable and an unset one are indistinguishable at install time, which is too late to find out.

Variables under the `ansible_` prefix that are not in the table are ignored. There are hundreds of them, they belong to Ansible's own connection plugins, and they are not cargoship's to police.

## Running cargoship as an Ansible module

The same translation runs inside a playbook. The cargoship binary is the Ansible module: it is installed as a set of symlinks named `cargoship_<action>`, and a symlink's name selects the action. There is no separate module package and nothing to install on the managed nodes.

Five modules ship, one per fleet action: `cargoship_apply`, `cargoship_prepare`, `cargoship_engine_config_sync`, `cargoship_reset`, and `cargoship_kube_config`. Package creation is not among them -- it builds an artifact from a definition in a repository, which belongs in a pipeline rather than in a convergence run.

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

### Installing the collection

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

The plugin forwards the four `ansible_*` connection variables in the table above and every `cargoship_*` variable, and drops the rest. A host's resolved variables routinely carry credentials for things that are not this cluster, and a module's parameters are written to a file on disk.

### Parameters every module takes

`inventory` takes the same document described above, `groups` and `hostvars` included. Everything else maps onto a flag of the command the module runs.

| Parameter | Flag | Notes |
| --- | --- | --- |
| `inventory` | | Required. The resolved Ansible inventory, plus `cluster`. |
| `inventory_path` | `--config` | Where to write the generated document. A private temporary file when unset. |
| `log_level` | `--log-level` | Defaults to `debug` when the play runs with `-v`. |
| `log_format` | `--log-format` | |

A parameter the module does not know is an error naming it. A parameter left unset is left unset: the module passes no flag for it, so whatever your cargoship configuration file sets still applies. That is why `label_nodes: false` and omitting `label_nodes` are different.

The surfaces differ because the commands do. `cargoship prepare` reads no encrypted value, so it takes no `vault_password_file`; `cargoship reset` installs nothing, so it takes no package and no `values`. A parameter offered to the wrong module is an error naming it, rather than a flag quietly dropped.

### `cargoship_apply`

Installs or converges the whole cluster.

| Parameter | Flag |
| --- | --- |
| `package` | the positional argument. Required. |
| `concurrency` | `--concurrency` |
| `work_concurrency` | `--work-concurrency` |
| `hosts` | `--hosts` |
| `firewall` | `--firewall` |
| `fapolicyd` | `--fapolicyd` |
| `label_nodes` | `--label-nodes` |
| `allow_unmanaged_nodes` | `--allow-unmanaged-nodes` |
| `update_kubeconfig` | `--update-kubeconfig` |
| `kubeconfig` | `--kubeconfig` |
| `values` | `--values`. A list of files. |
| `timeout` | `--timeout` |
| `vault_password_file` | `--vault-password-file` |
| `age_identity_file` | `--age-identity-file`. A list of files. |
| `public_key` | `--key` |
| `verify` | `--verify`. `never`, `if-possible`, or `always`. |

### `cargoship_prepare`

Stages a package onto the fleet and readies the hosts. Takes no key material.

| Parameter | Flag |
| --- | --- |
| `package` | the positional argument. Required. |
| `concurrency` | `--concurrency` |
| `work_concurrency` | `--work-concurrency` |
| `hosts` | `--hosts` |
| `firewall` | `--firewall` |
| `fapolicyd` | `--fapolicyd` |
| `values` | `--values`. A list of files. |
| `timeout` | `--timeout` |
| `public_key` | `--key` |
| `verify` | `--verify` |

### `cargoship_engine_config_sync`

Converges engine configuration across the fleet, which is the most Ansible-shaped thing cargoship does. It does not update the hosts themselves, so it takes none of the three host switches.

| Parameter | Flag |
| --- | --- |
| `package` | the positional argument. Required. |
| `concurrency` | `--concurrency` |
| `work_concurrency` | `--work-concurrency` |
| `label_nodes` | `--label-nodes` |
| `update_kubeconfig` | `--update-kubeconfig` |
| `kubeconfig` | `--kubeconfig` |
| `values` | `--values`. A list of files. |
| `timeout` | `--timeout` |
| `vault_password_file` | `--vault-password-file` |
| `age_identity_file` | `--age-identity-file`. A list of files. |
| `public_key` | `--key` |
| `verify` | `--verify` |

### `cargoship_reset`

Removes the cluster from the fleet. There is no package: the distro to remove is named directly.

| Parameter | Flag |
| --- | --- |
| `distro` | `--distro`. `k3s` or `rke2`. |
| `concurrency` | `--concurrency` |
| `work_concurrency` | `--work-concurrency` |
| `hosts` | `--hosts` |
| `firewall` | `--firewall` |
| `fapolicyd` | `--fapolicyd` |

### `cargoship_kube_config`

Fetches the cluster kubeconfig onto the management node. It is the one module that changes the node the play runs on rather than the fleet.

| Parameter | Flag |
| --- | --- |
| `distro` | `--distro`. `k3s` or `rke2`. |
| `kubeconfig` | `--kubeconfig` |

### Signature verification

`public_key` and `verify` are the package signature parameters, and they are the key-based ones only. Keyless verification identifies a signer against a Fulcio root and a transparency log, which needs network access a management node inside an airlock does not have. Verify a keyless-signed package with `cargoship package verify` before the play, where the flags and their exclusions are all available.

### Key material

Key material is always named by a path, never given by value. Ansible writes a module's parameters into a file on the managed node, and a password passed by value would be a password written to a file you did not choose the mode of. Set `no_log: true` on the task regardless: a binary module has no per-parameter `no_log`, so the task-level setting is what suppresses the arguments and the result.

### The `cluster` role

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

| Variable | Default | Meaning |
| --- | --- | --- |
| `cargoship_action` | `apply` | Which module to run. One of the five. |
| `cargoship_package` | unset | The package. Required for `apply`, `prepare`, and `engine_config_sync`. |
| `cargoship_cluster` | `{}` | The `cluster` block: name, load balancer, profiles, registries, values. |
| `cargoship_role_groups` | `{}` | The group mapping, when your groups are not named `controller` and `worker`. |
| `cargoship_inventory_path` | unset | Where to write the generated document. |
| `cargoship_args` | `{}` | Everything else, passed to the module as-is. |
| `cargoship_delegate_to` | `localhost` | The management node. |
| `cargoship_show_result` | `false` | Print `cargoship_result.cargoship` after the run. |

Every task in the role carries `run_once: true`. Cargoship converges the whole fleet in one run, so a play over the fleet's own inventory would otherwise run a full convergence once per host. It also carries `no_log: true`, which is why `cargoship_show_result` exists: the debug task is the one thing allowed to print, and it prints cargoship's report rather than the parameters.

### Check mode and changed

`--check` runs cargoship's dry run. Phases opt into it one at a time -- a phase that has not said how it behaves under a dry run is reported and not run -- so a check-mode task connects, detects, gathers facts, and validates the hosts, and reports the rest as phases it would have run.

Under check mode, `changed` means what Ansible means by it there: a real run would change something. It is true when a phase that knows how to tell had work outstanding -- a fleet whose engine configuration has drifted -- and false when every such phase found nothing to do.

`cargoship_kube_config` is the exception. `cargoship kube-config` has no dry run, so under `--check` the module reports `skipped: true` and runs nothing at all. That is what Ansible does with a module that has declared it does not support check mode; a binary module has nowhere to declare it, so it says so in its result instead.

### What `changed` is worth

`changed` is built from what the phases themselves report, and a phase has to opt in. `cargoship.changedSignal` says how much of the run is covered:

| Signal | Meaning |
| --- | --- |
| `complete` | Every phase the run reached said whether it changed anything. |
| `partial` | Some did not. They are named in `cargoship.changedUndeclared`. |
| `unknown` | No phase reported at all, so `changed` is a convention rather than an observation, and is reported `true`. |

Today the engine configuration sync phases report and the rest do not, so an ordinary run reads `partial`. That is deliberate: a phase that has said nothing cannot make `changed` true, and it is not treated as having changed nothing either. `KubeConfig` and `LabelNodes` are the two in the undeclared list that can genuinely change something -- the local kubeconfig and the node role labels -- so if a handler of yours depends on either, gate on the result rather than on `changed`.

`cargoship.phasesRan` and `cargoship.phasesPlanned` name the phases the run executed and, under check mode, the ones it reported instead of running. Ansible sees one result for the whole fleet, so these stand in for the per-host detail a task-per-host module would give you.

### The result

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

## Checking the result

The generated document is validated against the inventory schema before it is written, so a translation that produced something invalid fails here rather than ten minutes into an apply. Check it yourself as well, and read it: it is an ordinary cluster inventory, and it is the exact document cargoship installs from.

```sh
cargoship inventory from-ansible ./resolved.json -o ./inventory.yaml
cargoship validate ./inventory.yaml
cargoship apply ./package.tar.zst --config ./inventory.yaml --confirm
```

The generated file carries connection details and is written with mode `0600`.
