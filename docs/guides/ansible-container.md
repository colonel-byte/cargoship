# Running the Collection from the Container

The `-ansible` image is a management node in a container: `ansible-core` from the `alpine/ansible` base, plus the cargoship apk, which carries the binary and the `colonel_byte.cargoship` collection. It exists so that a fleet can be converged from a playbook without staging Ansible, the collection and the binary onto a VM by hand. This guide covers what has to be mounted into it, why the package mount is read-only, and the two things that break a run before it reaches the fleet: the UID the container runs as, and host key checking.

For the modules themselves -- their parameters and what `changed` means -- see [ansible-module](ansible-module.md), and for the inventory they translate, [ansible-inv](ansible-inv.md). Nothing about either changes inside the container.

```sh
podman pull ghcr.io/colonel-byte/cargoship:0.25.1-ansible
```

The image has no `ENTRYPOINT`, because a run legitimately reaches for `ansible-playbook`, `ansible-inventory`, `ansible-galaxy` or `cargoship` itself; name the command you want. The collection is installed to `/usr/share/ansible/collections`, which is already on ansible-core's default collections path, so `ansible-playbook` resolves `colonel_byte.cargoship.cargoship_apply` with no configuration. `HOME` is `/home/nonroot`, the working directory is `/workspace`, and the process runs as UID and GID 65532.

The image carries cargoship and the collection, and nothing for talking to the cluster afterwards. `kubernetes.core` is present because the base carries the full ansible distribution, but the Python client its modules import is not, so a `k8s` task fails at import time; there is no `helm` or `kubectl` either. A play that converges the cluster and then applies manifests to it needs an image of your own, built `FROM` this one with those added.

## Every Path in a Playbook Is a Path Inside the Container

Cargoship runs in the container and connects to the fleet from there, so `package`, `inventory_path`, `vault_password_file` and `ansible_ssh_private_key_file` all name paths the container can see -- not paths on the machine that started it. Mount each one at the same path it has on the host wherever you can. It costs nothing and it means the inventory reads the same whether the run happens in the container or on a management node that installed the package.

| What                                    | Where                                                           | Mode                |
| :-------------------------------------- | :-------------------------------------------------------------- | :------------------ |
| The distro package                      | wherever the playbook names it, e.g. `/srv/staging`             | read-only           |
| Playbooks, roles, the Ansible inventory | `/workspace`                                                    | read-only           |
| The SSH private key                     | wherever `ansible_ssh_private_key_file` names it                | read-only, `0600`   |
| The vault password file                 | wherever `vault_password_file` names it                         | read-only, `0400`   |
| `known_hosts`                           | `/home/nonroot/.ssh/known_hosts`                                | writable, see below |
| Where a kubeconfig is written           | `/home/nonroot/.kube/config`, or wherever `kubeconfig` names it | writable            |
| The cargoship cache                     | `/home/nonroot/.cargoship-cache`                                | writable, optional  |

An apply leaves `update_kubeconfig` on by default, which merges the cluster's admin credentials into `KUBECONFIG` when it is set and `~/.kube/config` otherwise -- inside the container, a file under `/home/nonroot` that goes away with the container. Mount a kubeconfig, name one with the `kubeconfig` parameter, or pass `update_kubeconfig: false` and fetch it deliberately with `cargoship_kube_config`; what you should not do is leave the default and wonder where the credentials went.

Everything under `/home/nonroot` is already created and owned by 65532 in the image, so a run with none of the optional mounts works -- it just starts cold each time and keeps nothing. The cache is the one worth adding first, and it has a section of its own below.

## The Package Mount Is Read-Only Because Nothing Writes to It

A distro package is the one input large enough that copying it into an image, or into the container's writable layer, is a real cost -- it carries every image and file the cluster installs. It is also read-only by nature: `cargoship apply` reads the package and writes to the fleet, never back into the artifact. Mounting it `:ro` costs nothing and turns a whole class of mistake, a run that mutates the staging directory every other run depends on, into an error at the moment it is attempted.

```sh
podman run --rm \
  --userns=keep-id:uid=65532,gid=65532 \
  -v /srv/staging:/srv/staging:ro,z \
  -v "$PWD":/workspace:ro,z \
  -v ~/.ssh/fleet_ed25519:/home/nonroot/.ssh/fleet_ed25519:ro,z \
  -v ~/.ssh/known_hosts:/home/nonroot/.ssh/known_hosts:z \
  -v ~/.cargoship-cache:/home/nonroot/.cargoship-cache:z \
  ghcr.io/colonel-byte/cargoship:0.25.1-ansible \
  ansible-playbook -i inventory.yaml converge.yaml
```

On a host with SELinux enforcing -- Fedora, RHEL, and anything derived from them -- a volume needs a label option or the container cannot read it. Use `z`, the shared label, for anything the host or another container also uses, which is all of the above. `Z` relabels the content privately to one container, and applying it to a shared staging directory takes that directory away from every other reader on the host, including the next container. Docker takes the same options; it is the label, not the runtime, that decides this.

## Mounting the Cache Read-Write

The cache is where cargoship keeps what it fetches -- OCI artifacts it pulls, and the release assets the example generation reads -- under `~/.cargoship-cache`, which is `/home/nonroot/.cargoship-cache` in the container. It is the only mount in this guide that exists purely to make the *next* run faster, and the only one that is worthless read-only: a cache that cannot be written to is a cache that is empty on every run, and a read-only mount turns what would be a slow run into a failed write.

```sh
podman run --rm \
  --userns=keep-id:uid=65532,gid=65532 \
  -v ~/.cargoship-cache:/home/nonroot/.cargoship-cache:rw,z \
  -v /srv/staging:/srv/staging:ro,z \
  -v "$PWD":/workspace:ro,z \
  ghcr.io/colonel-byte/cargoship:0.25.1-ansible \
  ansible-playbook -i inventory.yaml converge.yaml
```

`rw` is the default for a bind mount and is written out here only to say that it is meant: the package above it is `ro` on purpose, and the pair reads as a decision rather than an omission. Create the directory on the host first -- `mkdir -p ~/.cargoship-cache` -- because a bind mount whose source does not exist is created by the runtime as an empty directory owned by root, which the container cannot then write to.

The same UID rule as everywhere else decides whether the write succeeds, and there are three ways to satisfy it. `--userns=keep-id:uid=65532,gid=65532` maps your host UID onto the one the container runs as, which is the right answer when the cache lives in your home directory and you want to look at it afterwards. `chown -R 65532:65532 ~/.cargoship-cache` is the right answer under Docker or rootful Podman, where there is no mapping to arrange. Podman's `:U` option -- `-v ~/.cargoship-cache:/home/nonroot/.cargoship-cache:rw,z,U` -- recursively chowns the source to the container's user for you, which is convenient and also permanent, so do not point it at a directory anything else on the host owns.

A named volume avoids the question entirely, and is the better arrangement on a dedicated staging host where nobody needs to read the cache from outside:

```sh
podman volume create cargoship-cache
podman run --rm \
  -v cargoship-cache:/home/nonroot/.cargoship-cache \
  -v /srv/staging:/srv/staging:ro,z \
  -v "$PWD":/workspace:ro,z \
  ghcr.io/colonel-byte/cargoship:0.25.1-ansible \
  ansible-playbook -i inventory.yaml converge.yaml
```

A new named volume is populated from the image path it is mounted over, ownership included, so it arrives owned by 65532 with no mapping, no `chown` and no SELinux label option to choose. It survives `--rm`, and `podman volume rm cargoship-cache` is how you discard it deliberately.

To put the cache somewhere other than the default path, set it in the environment as `DISTRO_ZARF_CACHE`, or as `zarf_cache` in a cargoship configuration file -- the modules take no parameter for it, so a playbook cannot pass it per task. The `--zarf-cache` flag does the same thing for a `cargoship` command run directly in the container -- it is named for zarf's cache, which is what it is, and it takes cargoship's path. The default printed in `docs/commands` is the one this repository's own `cargoship-config.yaml` sets, not the built-in `~/.cargoship-cache` a container resolves, since viper searches the working directory for a configuration file and a checkout has one.

## The UID Is What Actually Breaks First

The image runs as 65532, and a private key mounted from your home directory is owned by you and readable only by you. Under rootless Podman your UID maps to root inside the container by default, so the key arrives owned by a user that is not the one reading it, and the run fails on a key it can see and cannot open. `--userns=keep-id:uid=65532,gid=65532` maps your host UID to 65532 inside, which makes every mount above land with an owner that matches the process.

Under Docker, or rootful Podman, there is no mapping to arrange: the file has to be readable by UID 65532 on the host. Give the staging directory and the key to that UID (`chown -R 65532:65532 /srv/staging`), which is the usual arrangement for a dedicated staging host. Running the image with `--user "$(id -u):$(id -g)"` instead looks simpler and is not: `/home/nonroot` and its subdirectories are owned by 65532, so the cache, `known_hosts` and Ansible's own temporary directory all become unwritable, and the failures arrive late and read like something else.

## Host Keys

Cargoship verifies host keys against `~/.ssh/known_hosts` -- `/home/nonroot/.ssh/known_hosts` in the container -- and appends a key it has not seen before, which is trust on first use. A fresh container has no such file, so with nothing mounted every host in the fleet is trusted on the first connection of every run, and the file recording that decision is thrown away with the container. That is weaker than it looks: it is not a first use, it is a first use each time.

Three ways out, in order of preference. Mount a `known_hosts` file writable, as above, so the first run records what it saw and every later run verifies against it. Or set `cargoship_host_key` on each host in the inventory, which states the expected key in the inventory itself and is the strongest of the three, since the answer travels with the host rather than with the container's filesystem. Or point `SSH_KNOWN_HOSTS` at a file you seeded beforehand; a read-only mount is fine here, but only because every key is already in it -- cargoship still has to be able to append when one is not, and a read-only file fails that write.

Setting `SSH_KNOWN_HOSTS=/dev/null` disables host key verification entirely. It is useful for a lab and is not a workaround for the paragraph above: a management node that accepts any host key accepts a machine standing in for a controller.

## SSH Agents Work, With the Socket Mounted

Cargoship reads `SSH_AUTH_SOCK` and offers the agent's keys, so an agent on the host serves a run in the container once its socket is mounted and the variable points at it.

```sh
podman run --rm \
  --userns=keep-id:uid=65532,gid=65532 \
  -v "$SSH_AUTH_SOCK":/run/ssh-agent.sock:z \
  -e SSH_AUTH_SOCK=/run/ssh-agent.sock \
  -v /srv/staging:/srv/staging:ro,z \
  -v "$PWD":/workspace:ro,z \
  ghcr.io/colonel-byte/cargoship:0.25.1-ansible \
  ansible-playbook -i inventory.yaml converge.yaml
```

An agent is the better arrangement when the key is protected by a passphrase, since nothing has to hold the passphrase inside the container, and it leaves no key file to mount read-only and forget about. It is worse when the run is unattended, because an agent is a session that has to be unlocked by somebody; a key file with a dedicated fleet key is the right answer there.

## Air-Gapped Runs Need No Network of Their Own

Everything the convergence installs comes out of the package, so the container needs a route to the fleet and to nothing else. It does not need a registry, a proxy, or a route off the management network, and the image pull is the only step in this guide that touches the internet -- do it on the machine that has a route, save the image with `podman save`, and load it where it runs.

## What Not to Mount

Do not mount the container runtime's socket, a whole home directory, or `/etc` from the host. The image needs four kinds of thing -- a package, a playbook tree, credentials for the fleet, and somewhere to write -- and each of them is better named individually than swept in with a parent directory. A resolved Ansible inventory already carries credentials for hosts that are not this cluster, which is why the collection's action plugin forwards only the variables cargoship reads; mounting your whole home directory undoes that care at the filesystem level.

Do not bake credentials into an image of your own with `COPY`. An image layer keeps what it is given even after a later layer removes it, and this image is published to a registry that the key's owner does not control.
