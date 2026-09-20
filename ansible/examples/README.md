# Cargoship Ansible Examples

Working inventories and playbooks for the `colonel_byte.cargoship` collection. Everything here is an example of the same arrangement: Ansible resolves the inventory, and cargoship runs on one management node and opens every SSH connection to the fleet itself. Ansible connects to nothing in the cluster, gathers no facts on it, and runs no task per host.

These files are outside the collection, so they are not installed with it. Copy what you need. The one playbook that does ship is `playbooks/example.yml` inside the collection itself.

The full reference is in two guides: every module parameter and what `changed` means in the [Ansible module guide](../../docs/guides/ansible-module.md), the host variable mapping and the group-to-role rules in the [Ansible inventory guide](../../docs/guides/ansible-inv.md). This directory is the part that is easier to read as a file than as a table.

## Inventories

| File                            | What it shows                                                                                                             |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `inventories/fleet.ini`         | The smallest inventory that installs a cluster: two groups, one variable each.                                            |
| `inventories/fleet/`            | The same fleet with `group_vars` and `host_vars`: profiles, labels, taints, a bastion, uploaded files, a pinned host key. |
| `inventories/custom-groups.yml` | Groups not named `controller` and `worker`, plus a host that is not part of the cluster.                                  |

Roles come from group membership. By default the group named `controller` supplies the control plane and the group named `worker` supplies the rest; `custom-groups.yml` needs a `roleGroups` mapping, which `playbooks/modules-direct.yml` supplies. Host order is load-bearing: controllers are written first and the first controller bootstraps the cluster.

## Playbooks

| File                               | What it runs                                                                                                      |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `playbooks/apply.yml`              | `cargoship apply`. Installs or converges the whole cluster, with profiles, a registry mirror, and package values. |
| `playbooks/prepare.yml`            | `cargoship prepare`. Stages the package and readies the hosts ahead of a maintenance window.                      |
| `playbooks/engine-config-sync.yml` | `cargoship engine-config-sync`. The run to put on a schedule, and the one whose `changed` is worth acting on.     |
| `playbooks/kube-config.yml`        | `cargoship kube-config`, then `kubernetes.core.k8s_info` against what it fetched.                                 |
| `playbooks/reset.yml`              | `cargoship reset`. Removes the cluster, behind a guard that has to be typed.                                      |
| `playbooks/modules-direct.yml`     | The same work without the role: `run_once`, `delegate_to`, `no_log`, and `roleGroups` spelled out.                |

Run one against an inventory:

```sh
ansible-playbook -i inventories/fleet playbooks/apply.yml
ansible-playbook -i inventories/fleet playbooks/apply.yml --check
ansible-playbook -i inventories/custom-groups.yml playbooks/modules-direct.yml
```

Every playbook here names paths under `/srv/staging` -- the package, the SSH keys, the vault password file. They are a staging directory's worth of placeholder, not a convention cargoship enforces; change them to yours.

## Encrypting Inventory Values with Ansible Vault

Nothing in `inventories/fleet/` is secret, but a real fleet's `group_vars` are: a registry credential, a token, the password behind a file you upload. Those go in an Ansible Vault file. Ansible resolves the inventory before cargoship is handed anything, so an encrypted value arrives already decrypted and the module never knows it was encrypted at all.

Vault encrypts whole files rather than parts of them, so the arrangement is to split a group's variables in two: what is worth reading in a diff stays in plaintext, the secrets go in a file of their own that is encrypted as a whole. Turn the existing `all.yml` into a directory to make room for both -- Ansible loads every file in a `group_vars/<group>/` directory and merges them:

```sh
cd inventories/fleet/group_vars
mkdir all
git mv all.yml all/main.yml
```

Write the password file somewhere outside the inventory and outside the repository, readable only by the account that runs the playbook:

```sh
install -m 0600 /dev/null /srv/staging/ansible-vault-pass
printf '%s' 'the-passphrase' > /srv/staging/ansible-vault-pass
```

Create the encrypted file and put the secrets in it. `create` opens `$EDITOR` and writes what you save already encrypted, so the plaintext never lands on disk:

```sh
ansible-vault create --vault-password-file /srv/staging/ansible-vault-pass all/vault.yml
```

```yaml
---
vault_mirror_user: mirror-svc
vault_mirror_password: the-registry-password
```

`ansible-vault edit` reopens it later, `view` prints it without an editor, `rekey` changes the password, and `encrypt` takes a plaintext file you already have and encrypts it in place.

The `vault_` prefix is a convention rather than a requirement. It keeps the encrypted names distinct from the names playbooks use, so the plaintext file can point at them and a reader can see where a value comes from without decrypting anything:

```yaml
# group_vars/all/main.yml
mirror_user: "{{ vault_mirror_user }}"
mirror_password: "{{ vault_mirror_password }}"
```

`playbooks/apply.yml` reads those same two names from the environment, because an example inventory has no vault to read them from. Delete its `vars:` block once the inventory supplies them -- otherwise it wins, because play variables outrank inventory ones -- and the `registries` credentials in `cargoship_cluster` resolve out of the vault instead.

Every run then needs the password:

```sh
ansible-playbook -i inventories/fleet playbooks/apply.yml \
  --vault-password-file /srv/staging/ansible-vault-pass
```

An `ansible.cfg` beside the playbooks says it once instead of on every command line:

```ini
[defaults]
vault_password_file = /srv/staging/ansible-vault-pass
```

`resolve-inventory.sh` needs the password too, since `ansible-inventory` cannot parse an encrypted `group_vars` file without it, and it takes it from the environment:

```sh
ANSIBLE_VAULT_PASSWORD_FILE=/srv/staging/ansible-vault-pass \
  ./resolve-inventory.sh inventories/fleet bubbles bubbles-kc.test.com > resolved.json
```

What that writes is plaintext: a vaulted `cargoship_` host variable is decrypted by the time it reaches the file. That is the same reason a translated inventory is written `0600` and belongs in a staging directory rather than in a repository.

None of this is the `vault_password_file` the playbooks pass in `cargoship_args`. That one is cargoship's own, and it decrypts `$ANSIBLE_VAULT` values sitting inside the cluster configuration at apply time; see the [vault encryption guide](../../docs/guides/vault-encryption.md). Ansible Vault on an inventory file is decrypted by Ansible, before the module is called. A fleet can use one, the other, or both, and the two passwords have no reason to match.

## Checking an Inventory Without a Fleet

`resolve-inventory.sh` does in the shell what the collection's action plugin does inside a playbook, so an inventory can be translated and checked before anything is installed:

```sh
./resolve-inventory.sh inventories/fleet bubbles bubbles-kc.test.com > resolved.json
cargoship inventory from-ansible resolved.json -o inventory.yaml
cargoship validate inventory.yaml
```

Read `inventory.yaml` afterwards. It is an ordinary cluster inventory, it is exactly the document a playbook would have installed from, and reading it once is the fastest way to see what a host variable did. A translated inventory carries connection details, so it is written with mode `0600` and belongs in a staging directory rather than in a repository.

The script needs `jq`. It is a convenience for checking these examples, not a step in any run.

## Before Any of This Runs

The collection has to be installed, and its module files have to point at the cargoship binary. The `.rpm`, `.deb`, and `.apk` packages do both, which is why a management node that installed cargoship from a package needs nothing further. Installing from a tarball or a checkout instead takes one more step, described under "Installing the collection" in the [Ansible module guide](../../docs/guides/ansible-module.md).

For a run inside a container rather than on a staging VM, see the [container guide](../../docs/guides/ansible-container.md): every path in these playbooks becomes a path that has to be mounted.
