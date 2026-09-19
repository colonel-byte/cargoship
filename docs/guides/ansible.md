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

One module ships today, `cargoship_engine_config_sync`. It converges engine configuration across the fleet, which is the most Ansible-shaped thing cargoship does.

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

### Parameters

`inventory` takes the same document described above, `groups` and `hostvars` included. Everything else maps onto a flag of `cargoship engine-config-sync`.

| Parameter | Flag | Notes |
| --- | --- | --- |
| `package` | the positional argument | Required. The distro package to synchronise from. |
| `inventory` | | Required. The resolved Ansible inventory, plus `cluster`. |
| `inventory_path` | `--config` | Where to write the generated document. A private temporary file when unset. |
| `concurrency` | `--concurrency` | |
| `work_concurrency` | `--work-concurrency` | |
| `label_nodes` | `--label-nodes` | |
| `update_kubeconfig` | `--update-kubeconfig` | |
| `kubeconfig` | `--kubeconfig` | |
| `values` | `--values` | A list of files. |
| `timeout` | `--timeout` | |
| `vault_password_file` | `--vault-password-file` | |
| `age_identity_file` | `--age-identity-file` | A list of files. |
| `log_level` | `--log-level` | Defaults to `debug` when the play runs with `-v`. |
| `log_format` | `--log-format` | |

A parameter the module does not know is an error naming it. A parameter left unset is left unset: the module passes no flag for it, so whatever your cargoship configuration file sets still applies. That is why `label_nodes: false` and omitting `label_nodes` are different.

Key material is always named by a path, never given by value. Ansible writes a module's parameters into a file on the managed node, and a password passed by value would be a password written to a file you did not choose the mode of. Set `no_log: true` on the task regardless: a binary module has no per-parameter `no_log`, so the task-level setting is what suppresses the arguments and the result.

### Check mode and changed

`--check` runs cargoship's dry run. Phases opt into it one at a time -- a phase that has not said how it behaves under a dry run is reported and not run -- so a check-mode task connects, detects, gathers facts, and validates the hosts, and reports the rest as phases it would have run.

Under check mode, `changed` means what Ansible means by it there: a real run would change something. It is true when a phase that knows how to tell had work outstanding -- a fleet whose engine configuration has drifted -- and false when every such phase found nothing to do.

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
