# Roles

| Role                                         | What it does                                                                           |
| -------------------------------------------- | -------------------------------------------------------------------------------------- |
| [`packages`](role_packages.md)               | Queries the cluster via `zarf package list` and sets the `zarf_packages` fact.         |
| [`package_inspect`](role_package_inspect.md) | Inspects a Zarf package definition and sets structured metadata facts.                  |
| [`state`](role_state.md)                     | Queries the cluster for the `zarf-state` Secret and sets the parsed `zarf_state` fact. |

## packages

`packages` queries installed Zarf packages and exposes them as a structured list under the `zarf_packages` fact:

```yaml
- name: List installed Zarf packages
  ansible.builtin.include_role:
    name: colonel_byte.zarf.packages
  vars:
    zarf_kubeconfig: /etc/rancher/rke2/rke2.yaml

- name: Report installed packages
  ansible.builtin.debug:
    msg: "{{ zarf_packages | map(attribute='package') | list }}"
```

## package_inspect

`package_inspect` parses a package tarball, OCI reference, or cluster package definition and sets facts.

Naming `zarf_public_key` requires a signature. That is the role being stricter than the module, which passes `verify` through and leaves zarf's own `if-possible` default alone -- and `if-possible` accepts an unsigned package whether or not a key was given. Set `zarf_verify` explicitly to override, including to `if-possible` to get zarf's default back.

```yaml
- name: Inspect storage package definition, rejecting it when unsigned
  ansible.builtin.include_role:
    name: colonel_byte.zarf.package_inspect
  vars:
    zarf_package: /srv/staging/zarf-package-csi-rook-ceph-amd64-v1.20.7-upstream.tar.zst
    zarf_public_key: /etc/zarf/colonel-byte-zarf-packages.pub

- name: Report package details
  ansible.builtin.debug:
    msg:
      name: "{{ zarf_package_name }}"
      version: "{{ zarf_package_version }}"
      flavor: "{{ zarf_package_flavor }}"
```

## state

`state` is the convenient way to retrieve and parse Zarf's cluster state. It runs `zarf tools kubectl get secret zarf-state -n zarf -o jsonpath='{.data.state}'`, decodes the payload, and sets the resulting dictionary as the `zarf_state` fact.

The credentials in that Secret -- the registry push, pull and seed secrets, the git server passwords, the artifact server token, and the agent webhook's TLS private key -- are withheld. The paths withheld are set as `zarf_state_redacted`, and `zarf_include_credentials: true` returns them instead. The role defaults `no_log` on both of its tasks, which is also why reaching for the role beats calling the module directly here: `set_fact` prints what the module censored, so the module alone cannot keep a credential out of a `-v` transcript. See [choice-zarf-info-modules](../../agent/choice-zarf-info-modules.md).

```yaml
- name: Load cluster zarf state
  ansible.builtin.include_role:
    name: colonel_byte.zarf.state
  vars:
    zarf_kubeconfig: /etc/rancher/rke2/rke2.yaml

- name: Inspect cluster registry mode
  ansible.builtin.debug:
    msg: "Cluster registry mode is {{ zarf_state.registryInfo.registryMode }}"

- name: Report which paths were withheld
  ansible.builtin.debug:
    var: zarf_state_redacted
```
