# Roles

| Role                         | What it does                                         |
| ---------------------------- | ---------------------------------------------------- |
| [`cluster`](role_cluster.md) | Runs one cargoship action from an Ansible inventory. |

`cluster` is the shorter way to write a module call. It picks the module from `cargoship_action`, assembles the `inventory` parameter from the play's `groups` and `hostvars`, and sets `run_once`, `delegate_to`, and `no_log` for you.

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

Anything the role does not name a variable for goes in `cargoship_args`, which is passed to the chosen module verbatim. The [module pages](modules.md) list what each module takes.

## There is only one

Every role here drives the cargoship binary from the management node. A role that configures fleet hosts directly -- writing files on them, installing packages, managing services -- does not belong in this collection, however convenient it would be to ship alongside; it belongs in the playbook that consumes the collection. That boundary is what lets the collection declare a controller range and no managed-node floor.
