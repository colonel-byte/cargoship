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

`package_inspect` parses a package tarball, OCI reference, or cluster package definition and sets facts:

```yaml
- name: Inspect storage package definition
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

`state` is the convenient way to retrieve and parse Zarf's cluster state. It runs `zarf tools kubectl get secret zarf-state -n zarf -o jsonpath='{.data.state}'`, decodes the payload, and sets the resulting dictionary as the `zarf_state` fact:

```yaml
- name: Load cluster zarf state
  ansible.builtin.include_role:
    name: colonel_byte.zarf.state
  vars:
    zarf_kubeconfig: /etc/rancher/rke2/rke2.yaml

- name: Inspect cluster registry mode
  ansible.builtin.debug:
    msg: "Cluster registry mode is {{ zarf_state.registryInfo.registryMode }}"
```
