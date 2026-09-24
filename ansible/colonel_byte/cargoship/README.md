# colonel_byte.cargoship

Install and converge air-gapped Kubernetes clusters with [cargoship](https://github.com/colonel-byte/cargoship), driven from an Ansible inventory you already have.

Ansible supplies the inventory and nothing else. It does not connect to the fleet, gather facts on it, or run a task per host. Every task in this collection runs on one management node outside the cluster -- the node the distro package and images were staged onto -- and cargoship opens every SSH connection itself from there.

## Invocation Patterns: Role vs Direct Modules

This collection offers two ways to run Cargoship actions: the `cluster` role (recommended) and calling the modules directly.

| Style                                                            | When to use                                                     | Key characteristics                                                                                                                                                                       |
| ---------------------------------------------------------------- | --------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **`include_role: colonel_byte.cargoship.cluster`** (Recommended) | Standard playbooks converging clusters from an inventory        | High-level ergonomic wrapper. Automatically handles `run_once: true`, `delegate_to: localhost`, credential protection with `no_log: true`, and automatic inventory assembling/projection. |
| **`colonel_byte.cargoship.cargoship_<action>`**                  | Custom automation pipelines, scripts, or fine-grained workflows | Low-level execution primitives. You must explicitly set `delegate_to`, `run_once: true`, `no_log: true`, and provide the `inventory` parameter.                                           |

### Why `include_role` is the default choice

Cargoship is designed to converge an entire fleet concurrently in a single execution. In a standard play run against `hosts: all`, calling a module directly without `run_once: true` would trigger a full cluster convergence once per host in the fleet. The `cluster` role applies `run_once: true`, ensures tasks delegate to the management node, masks sensitive parameters in logs, and maps the action dynamically via `cargoship_action`.

## Modules

| Module                         | Command                        |
| ------------------------------ | ------------------------------ |
| `cargoship_apply`              | `cargoship apply`              |
| `cargoship_prepare`            | `cargoship prepare`            |
| `cargoship_engine_config_sync` | `cargoship engine-config-sync` |
| `cargoship_reset`              | `cargoship reset`              |
| `cargoship_kube_config`        | `cargoship kube-config`        |

Each module takes an `inventory` argument holding Ansible's resolved `groups` and `hostvars`, plus a `cluster` block for the settings no host carries. The action plugin fills `groups` and `hostvars` in from the play, so a playbook writes only `cluster`.

Roles come from group membership: the group named `controller` supplies the control plane and the group named `worker` supplies the rest, remappable with `roleGroups`. Host order decides which node bootstraps the cluster.

## The role

`colonel_byte.cargoship.cluster` wraps any one of the modules. See `roles/cluster/defaults/main.yml` for its variables and `playbooks/example.yml` for a run. Fuller examples -- an inventory per shape, a playbook per module -- are in [ansible/examples](../../examples) in the repository.

```yaml
- ansible.builtin.include_role:
    name: colonel_byte.cargoship.cluster
  vars:
    cargoship_action: apply
    cargoship_package: /srv/staging/rke2-1.31.tar.zst
    cargoship_cluster:
      name: bubbles
      loadbalancer: bubbles-kc.test.com
```

## Installing

The cargoship `.rpm`, `.deb`, and `.apk` packages install this collection and its module symlinks. Nothing else is needed on the management node, and nothing at all is needed on the fleet.

For any other installation route, see [plugins/modules/README.md](plugins/modules/README.md).

## Documentation

The full parameter reference is in the [collection reference](https://colonel-byte.github.io/cargoship/ansible/collection.html), which carries a page per module and per role. What `changed` means, and how the collection is installed from a tarball, are in the [Ansible module guide](https://colonel-byte.github.io/cargoship/guides/ansible-module.html). The host variable mapping and the group-to-role rules are in the [Ansible inventory guide](https://colonel-byte.github.io/cargoship/guides/ansible-inv.html).
