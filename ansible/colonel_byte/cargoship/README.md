# colonel_byte.cargoship

Install and converge air-gapped Kubernetes clusters with [cargoship](https://github.com/colonel-byte/cargoship), driven from an Ansible inventory you already have.

Ansible supplies the inventory and nothing else. It does not connect to the fleet, gather facts on it, or run a task per host. Every task in this collection runs on one management node outside the cluster -- the node the distro package and images were staged onto -- and cargoship opens every SSH connection itself from there.

## Modules

| Module | Command |
| --- | --- |
| `cargoship_apply` | `cargoship apply` |
| `cargoship_prepare` | `cargoship prepare` |
| `cargoship_engine_config_sync` | `cargoship engine-config-sync` |
| `cargoship_reset` | `cargoship reset` |
| `cargoship_kube_config` | `cargoship kube-config` |

Each module takes an `inventory` argument holding Ansible's resolved `groups` and `hostvars`, plus a `cluster` block for the settings no host carries. The action plugin fills `groups` and `hostvars` in from the play, so a playbook writes only `cluster`.

Roles come from group membership: the group named `controller` supplies the control plane and the group named `worker` supplies the rest, remappable with `roleGroups`. Host order decides which node bootstraps the cluster.

## The role

`colonel_byte.cargoship.cluster` wraps any one of the modules. See `roles/cluster/defaults/main.yml` for its variables and `playbooks/example.yml` for a run.

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

The full parameter reference, the host variable mapping, and what `changed` means here are in the cargoship [Ansible guide](https://github.com/colonel-byte/cargoship/blob/main/docs/guides/ansible.md).
