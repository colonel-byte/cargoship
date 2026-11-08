# Generating an Inventory from Ansible

Cargoship can build its cluster inventory from an Ansible inventory you already have, deriving each host's role from the Ansible groups it belongs to. This guide covers that translation: the group mapping, the host variables cargoship reads, and the rules the translation follows. The same translation runs inside a playbook, where cargoship is the Ansible module; the modules, the collection, and the `cluster` role are covered in [Running Cargoship as an Ansible Module](ansible-module.md).

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

| Inventory field    | Variable                       | Default                |
| ------------------ | ------------------------------ | ---------------------- |
| `ssh.address`      | `ansible_host`                 | the inventory hostname |
| `ssh.user`         | `ansible_user`                 | `root`                 |
| `ssh.port`         | `ansible_port`                 | `22`                   |
| `ssh.keyPath`      | `ansible_ssh_private_key_file` | unset                  |
| `ssh.bastion`      | `cargoship_bastion`            | unset                  |
| `hostname`         | `cargoship_hostname`           | the inventory hostname |
| `profile`          | `cargoship_profile`            | the host's role        |
| (a profile's body) | `cargoship_profile_spec`       | unset                  |
| `privateAddress`   | `cargoship_private_address`    | unset                  |
| `privateInterface` | `cargoship_private_interface`  | unset                  |
| `engine.labels`    | `cargoship_node_labels`        | unset                  |
| `engine.taints`    | `cargoship_node_taints`        | unset                  |
| `host`             | `cargoship_host`               | unset                  |
| `environment`      | `cargoship_environment`        | unset                  |
| `files`            | `cargoship_files`              | unset                  |

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

### A profile can be declared where it is selected

`cargoship_profile` says which profile a host takes, and `cargoship_profile_spec` says what that profile is. The second is optional: a profile's body may also be written in the playbook's cluster block, under `profiles`, which is where it had to go before.

Writing it here is what keeps the two together. `cargoship_profile` belongs in the `group_vars` of the group that carries the profile, so without the spec the selection is in one file and the body is in another, and adding a node group means editing both. With it, `group_vars/controller.yml` is the whole truth about controllers:

```yaml
cargoship_profile: control
cargoship_profile_spec:
  host:
    ports:
      - port: "6443"
        protocol: tcp
  engine:
    labels:
      adrp.xyz/purpose-control: "true"
```

The spec takes the shape the inventory schema documents for `.spec.config.profiles[<name>]` -- `host`, `engine`, and `concurrency` -- and it lands under the name the host selects. A host that names no profile takes one named for its role, so a spec in `group_vars/worker.yml` with no `cargoship_profile` beside it defines the `worker` profile.

A profile defined twice with different bodies is an error naming both sources, whether the second is another host or the cluster block. There is no correct body to pick, and picking one silently would configure nodes in a way nobody wrote down -- the same reason a host in two groups that disagree about its role is refused rather than resolved. Two definitions that agree are not a conflict, so repeating a spec across several `group_vars` files is safe, and so is leaving a copy in the cluster block while one is being moved out of it.

### Misspelled variables are errors

A variable under the `cargoship_` prefix that cargoship does not read is an error, and so is an unknown key inside one of the structured variables. Ansible has no notion of a variable belonging to anyone, so a misspelled `cargoship_profil` would otherwise sit in `hostvars` looking set and doing nothing, and a misspelled key would be dropped on the way in. A misspelled variable and an unset one are indistinguishable at install time, which is too late to find out.

Variables under the `ansible_` prefix that are not in the table are ignored. There are hundreds of them, they belong to Ansible's own connection plugins, and they are not cargoship's to police.

That error arrives at install time, which is the latest useful moment rather than the earliest. The section below moves it into the editor.

## Editor completion for host and group variables

Cargoship publishes a JSON Schema for the variables above, so an editor completes them as you type and reports a misspelling on the line that holds it. It is the same arrangement the [inventory guide](setup-inv.md) describes for a `Cluster` document, pointed at a different file.

```sh
cargoship schema hostvars -o ./cargoship-hostvars.schema.json
```

The schema is served out of the binary, so this needs no network -- which is the point, on a management node that has none. Then reference it from the top of each `host_vars` or `group_vars` file:

```yaml
---
# yaml-language-server: $schema=../../cargoship-hostvars.schema.json
cargoship_profile: infra
cargoship_profile_spec:
  engine:
    taints:
      - adrp.xyz/infra=true:NoSchedule
```

Each variable is typed against the field it configures, so `cargoship_host` completes as a host configuration, `cargoship_profile_spec` as a profile, and `cargoship_bastion` as an SSH block -- the same definitions the cluster schema publishes. A misspelled `cargoship_profil`, or a `cargoship_host_key` that cargoship has never read, is reported as `Property cargoship_profil is not allowed` rather than surviving to install time.

Variables belonging to other roles are left alone. A `host_vars` file legitimately carries settings for everything in the play, so anything outside the `cargoship_` and `ansible_` names above is accepted without comment. Only a key under the `cargoship_` prefix that cargoship does not read is refused, which is the same rule the translation applies, moved earlier.

### Where the schema file may live, and where it must not

**Do not put the `.json` file inside an inventory directory.** Ansible's `yaml_valid_extensions` includes `.json` by default, and two of the three obvious placements break -- one of them silently.

| Location                                       | What Ansible does with it                                                                                                                                                                               |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Beside the inventory, outside it               | Nothing. This is the one to use.                                                                                                                                                                        |
| The inventory root, next to `hosts.yml`        | Parses it as an **inventory source**. A schema's top-level keys become groups, so you get a group named `properties` and warnings like `Skipping '$schema' as this is not a valid group definition`.    |
| A flat `host_vars/` or `group_vars/` directory | Nothing. The vars plugin matches files by entity name, and no host is called `cargoship-hostvars.schema`. Safe, but it is safe by accident -- a file named after a host would be read as its variables. |
| A per-host directory, `host_vars/<hostname>/`  | **Loads it as variables, silently.** Ansible reads every file in a per-host directory, so `$schema`, `type`, `properties` and `$defs` all become variables on that host. There is no warning.           |

The last row is the one worth remembering. Cargoship itself is unharmed -- the action plugin forwards only `ansible_` and `cargoship_` names, so the junk never reaches the module, and nothing in the generated inventory changes -- but every other role in the play sees those variables, and nothing anywhere says so.

Keep the schema out of the inventory entirely and reference it by relative path. The example inventory does this: `ansible/examples/inventories/fleet/host_vars/kw03.yml` points five levels up at the repository's own `schema/` directory.

## Checking the result

The generated document is validated against the inventory schema before it is written, so a translation that produced something invalid fails here rather than ten minutes into an apply. Check it yourself as well, and read it: it is an ordinary cluster inventory, and it is the exact document cargoship installs from.

```sh
cargoship inventory from-ansible ./resolved.json -o ./inventory.yaml
cargoship validate ./inventory.yaml
cargoship apply ./package.tar.zst --config ./inventory.yaml --confirm
```

The generated file carries connection details and is written with mode `0600`.

A playbook does not run these commands itself. The modules write and validate the same document, and the path they write it to is the `inventory_path` parameter described in [Running Cargoship as an Ansible Module](ansible-module.md).
