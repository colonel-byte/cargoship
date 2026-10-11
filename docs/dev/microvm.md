# A local VM fleet

`mage dev:vmUp` brings up a fleet of Rocky 10 virtual machines on your own machine and writes a `ZarfCluster` inventory pointing at them, so that `cargoship prepare` and `cargoship apply` can be run against hosts with a real kernel. `mage dev:vmDown` takes it away again.

It exists for the phases the [cluster e2e suite](e2e-tests.md) cannot reach. Those nodes are containers, and a container reports no SELinux, runs no fapolicyd and has no firewall of its own, so [`pkg/phase/21_prepare_selinux.go`](https://github.com/colonel-byte/cargoship/blob/main/pkg/phase/21_prepare_selinux.go) and its neighbours correctly do nothing there. Testing a change to any of them means a virtual machine. [choice-microvm-backend](../agent/choice-microvm-backend.md) records why this is plain qemu rather than Firecracker or libvirt.

Nothing it does touches the host. It needs no root, starts no daemon, and changes no SELinux, firewall or kernel setting on your machine - which is the point, since the configuration under investigation is usually the host's own.

## What you need

`qemu-system-x86_64`, `qemu-img`, `mkfs.vfat`, `mcopy`, `ssh`, and a readable `/dev/kvm`. `mage dev:vmUp` reports every missing tool at once rather than failing part way through a bring-up.

[qemu-setup](qemu-setup.md) is the full walkthrough: which package provides each of those on Fedora and Debian, how `/dev/kvm` access works without joining a group, what to do when your development machine is itself a virtual machine, and how to tell a node is really accelerated rather than emulated.

## Bringing a fleet up

```sh
mage dev:vmUp -control=1 -worker=2
mage dev:vmUp -control=1 -worker=2 -infra=3 -distro=rke2
```

Every flag has a default, so `mage dev:vmUp` on its own gives one control-plane node and two workers running k3s.

| Flag        | Default | What it does                                                                                    |
| ----------- | ------- | ----------------------------------------------------------------------------------------------- |
| `-control`  | `1`     | Control-plane nodes. The first is the leader, which is what the inventory's load balancer names |
| `-worker`   | `2`     | Plain worker nodes                                                                              |
| `-infra`    | `0`     | Infra nodes: workers carrying an `infra` profile of their own                                   |
| `-distro`   | `k3s`   | The engine the fleet is prepared for, `k3s` or `rke2`                                           |

`-distro` changes nothing about the guests. It decides only which of the leader's ports are forwarded to your machine, since rke2 serves its join endpoint on 9345 and k3s multiplexes onto 6443.

Infra nodes join as workers -- the engine has no third role -- and what makes them infra is their profile, which [`pkg/phase/81_label_nodes.go`](https://github.com/colonel-byte/cargoship/blob/main/pkg/phase/81_label_nodes.go) turns into a `node-role.kubernetes.io/infra` label and which a cluster config can hang a per-profile concurrency off. They are worth asking for because every other host in the generated inventory has a profile that just restates its role, which leaves both of those mechanisms indistinguishable from the role they came from. Ask for some and the engine phases batch them separately: `starting agent (profile: infra)`.

The first run downloads a pinned Rocky 10 GenericCloud image, roughly 520MiB, into `$XDG_CACHE_HOME/cargoship/microvm` and verifies its digest. That cache is outside the repository and survives a clean checkout; later runs reuse it.

Each node is a copy-on-write overlay over that image, so a bring-up costs a few megabytes rather than another copy. Bring-up takes a minute or two: a node answers SSH about six seconds in, and the rest is cloud-init installing firewalld and fapolicyd. The fleet is only handed back once cloud-init has finished on every node, because the gates in phases 22 and 26 ask whether those services are *running* - a fleet returned any earlier looks healthy and has both of those phases silently skipping.

```
NODE  ROLE        STATE                 SSH              PRIVATE ADDRESS
kc0   controller  running (pid 197656)  127.0.0.1:26241  10.73.156.11
kw0   worker      running (pid 197673)  127.0.0.1:26242  10.73.156.12
kw1   worker      running (pid 197714)  127.0.0.1:26243  10.73.156.13

Inventory: build/microvm/dev/inventory.yaml
```

Then drive cargoship at it the way you would any other inventory:

```sh
cargoship prepare --config build/microvm/dev/inventory.yaml ./example/rke2-multi-cni-cilium/v1_36/v1.36.1-rke2r1
cargoship apply   --config build/microvm/dev/inventory.yaml ./example/rke2-multi-cni-cilium/v1_36/v1.36.1-rke2r1
```

## Looking around, and tearing down

```sh
mage dev:vmList         # what is there and whether it is running
mage dev:vmShell kc0    # a root shell on one node
mage dev:vmDown         # every node stopped, every trace removed
mage dev:vmImage        # fetch and verify the base image, start nothing
```

Each of those takes `-fleet` as well, and it matters once the e2e suite has been run against VMs: `CARGOSHIP_E2E_BACKEND=microvm` brings up a fleet named `e2e`, which the default `dev` fleet does not cover. A suite run killed before its own teardown leaves that fleet behind, and nothing else will remove it:

```sh
mage dev:vmList -fleet=e2e
mage dev:vmDown -fleet=e2e
```

`dev:vmUp` refuses a fleet that is already up rather than adopting it, so every bring-up starts from a fresh overlay. Tear the old one down first. That refusal is deliberate: a node still carrying the state of a previous apply is the one thing that makes a phase failure impossible to reason about.

Note that `mage dev:clean` does not remove a fleet. It removes built binaries, and deleting a live fleet's directory from under it would leave its qemu processes running with no record of them. `dev:vmDown` is what takes a fleet away.

## How the networking works

Each node has two interfaces, and which is which matters when reading a phase's output.

`enp0s3` is the management interface, a qemu `user` (slirp) network. It carries the SSH forward your machine connects through, and outbound access to the internet - which phase 21 depends on, since it installs `container-selinux` by name and fails the run if it cannot. Every node has the same address on it, `10.0.2.15`, and no node can reach another there.

`enp0s4` is the fleet's own layer 2 segment, carried between the qemu processes on a multicast socket. This is where the nodes see each other, on `10.73.<fleet>.<node>`, and it is what the inventory names as each host's `privateAddress` and `privateInterface`. Those two fields are set explicitly rather than discovered, because the default route is on the management interface: fact gathering would pick `10.0.2.15` for every host, and the firewall and `/etc/hosts` phases would faithfully distribute an address no peer can reach.

The leader additionally forwards its engine API port to your machine, so `cargoship kube-config` and `kubectl` work from here rather than only from inside the fleet.

## When a node does not come up

A node that never answers is reported with the tail of its serial console, and the full log is at `build/microvm/dev/<node>/console.log`. That is the only record: qemu is detached, so nothing else captured its boot.

A node whose cloud-init failed is reported with cloud-init's own explanation, from `cloud-init status --long`. Run that on the node yourself for more, and read `/var/log/cloud-init.log`.

Host keys are regenerated on every bring-up, so host key checking is off and known-hosts is `/dev/null` for every connection the fleet makes. Set `SSH_KNOWN_HOSTS=""` when running `cargoship` against the inventory, for the same reason.

## What the management node can and cannot reach

Your machine reaches a node on exactly one path: the forwarded SSH port. That is enough for every phase cargoship drives over SSH, which is all of the host preparation, the uploads, the engine install and the join -- and it is all this fleet sets out to cover.

It is not enough for the phases that talk to the Kubernetes API from the management node. Two independent things stop them:

*   The inventory's `loadbalancer` is the leader's address on the fleet's private segment, because that is the address the other nodes join through. Nothing outside the qemu processes can route to that segment, so a phase that dials the load balancer from here gets a timeout.
*   Once `--firewall` has run, firewalld's `public` zone -- which holds both of a node's interfaces -- allows ssh and nothing else. Node-to-node traffic works because the trusted zone admits the private addresses by source, so the leader's forwarded API port reaches the guest and is then dropped. This is the firewall behaving correctly.

In practice that means `cargoship apply --label-nodes` fails at `Labeling nodes with their profile group`, and a kubeconfig written for this fleet does not work from here. Drive `kubectl` from inside a node instead:

```sh
mage dev:vmShell kc0
export KUBECONFIG=/etc/rancher/rke2/rke2.yaml   # or /etc/rancher/k3s/k3s.yaml
/var/lib/rancher/rke2/bin/kubectl get nodes -o wide
```

Giving the management node a routable path onto the private segment would mean a tap device or a bridge, which needs root and puts the fleet back under the host's firewall -- the two things [choice-microvm-backend](../agent/choice-microvm-backend.md) picked a multicast socket to avoid.

## The engine's node IP

Each node reports its own address on the private segment, which is what makes a multi-controller cluster possible here at all. That was not always true, and the story is worth knowing because this fleet is what found it.

Cargoship used to render no `node-ip`, so the engine chose its own node address from whichever interface held the default route -- the management one. Every node then reported `10.0.2.15`, the same slirp address, and a three-controller cluster could not form: all three advertised the same etcd peer URL, so the second could never be added and the apply sat on `MemberAdd request timed out` until it gave up. A single controller hid it completely. The API server also could not reach a kubelet it believed was at an address shared with every other node, so `kubectl logs` and `kubectl exec` returned `502 Bad Gateway` and a Helm-installed CNI could fail outright, leaving every node `NotReady`.

`ConfigureEngine` now writes `node-ip` from the host's `privateAddress`, beside the `node-name` it already wrote, so the fleet behaves the way any multi-homed fleet should:

```console
$ kubectl get nodes -o wide
NAME   STATUS   ROLES                INTERNAL-IP
kc0    Ready    control-plane,etcd   10.73.156.11
kw0    Ready    <none>               10.73.156.12
kw1    Ready    <none>               10.73.156.13
```

## Driving the OpenTofu provider at a fleet

The provider takes the same hosts, so a fleet is also how `cargoship_cluster` gets exercised against real machines rather than one container. `mage dev:vmUp` writes a `ZarfCluster` inventory, and every field a `hosts` entry needs is in it:

| Inventory             | `hosts` entry        | For the fleet                                        |
| --------------------- | -------------------- | ---------------------------------------------------- |
| `ssh.address`         | `address`            | Always `127.0.0.1` -- the forwards bind loopback     |
| `ssh.port`            | `port`               | The per-node forwarded SSH port                      |
| `ssh.user`            | `user`               | `root`                                               |
| `ssh.keyPath`         | `key_path`           | The fleet key, under `build/microvm/<fleet>/key`     |
| `hostname`            | `hostname`           | `kc0`, `kw0`, ...                                    |
| `privateAddress`      | `private_address`    | The node's address on the private segment            |
| `privateInterface`    | `private_interface`  | `enp0s4`                                             |
| `role` / `profile`    | `role` / `profile`   | Unchanged                                            |

```hcl
resource "cargoship_cluster" "fleet" {
  name          = "microvm-dev"
  load_balancer = "10.73.156.11"            # the leader's private address
  package       = "./build/cargoship-distro-amd64.tar.zst"

  hosts = {
    kc0 = {
      address           = "127.0.0.1"
      port              = 26241
      user              = "root"
      key_path          = "/path/to/build/microvm/dev/key"
      hostname          = "kc0"
      private_address   = "10.73.156.11"
      private_interface = "enp0s4"
      role              = "controller"
      profile           = "controller"
    }
    kw0 = {
      address           = "127.0.0.1"
      port              = 26242
      user              = "root"
      key_path          = "/path/to/build/microvm/dev/key"
      hostname          = "kw0"
      private_address   = "10.73.156.12"
      private_interface = "enp0s4"
      role              = "worker"
      profile           = "worker"
    }
  }
}
```

Four things to get right, each of which is a property of the fleet rather than of the provider:

*   **`key_path` must be absolute.** The inventory already holds an absolute path for this reason; OpenTofu runs from wherever you invoke it.
*   **`load_balancer` is the leader's private address**, not a forwarded loopback port. It is the address the other nodes join through, and they reach each other on the private segment rather than through your machine.
*   **Export `SSH_KNOWN_HOSTS=""`** before `tofu apply`. Host keys are regenerated on every bring-up, so a known-hosts entry is wrong by construction.
*   **Leave `export_kubeconfig` and `label_nodes` off.** Both need the management node to reach the API server, which it cannot on a fleet whose load balancer is private -- see above. A provider run that sets either will fail there rather than in anything it is testing.

The `hosts` key orders the run: controllers are acted on first, and the controller whose key sorts first becomes the leader. Keying the map by the fleet's own hostnames keeps that in step with how `mage dev:vmUp` numbers them.

Bring the fleet up, generate the block from the inventory, and `tofu apply`. A removal is the other half: set a host's `state = "absent"` and the provider drains it, deletes it from the cluster and uninstalls the engine, which is the same work [e2e-external-inventory](e2e-external-inventory.md) drives through the CLI. [tofu-provider](tofu-provider.md) covers building and running the provider itself.

## The shipped k3s packages, and what is still unproven

Two separate things used to stop a k3s cluster coming up, and only one of them is fixed.

**Fixed: the join URL named the wrong port.** `ConfigureEngine` wrote `server: https://<host>:9345` for both engines. 9345 is rke2's -- it runs its supervisor separately -- while k3s multiplexes its supervisor onto 6443 and listens on nothing at 9345. `K3S` embeds the shared Rancher configuration without overriding that, so every k3s cluster was handed a join URL its own engine was not serving, and nothing could join. The port now comes from the distro.

**Not fixed, and not retested since: the SELinux policy is staged and never installed.** Every `example/k3s-*` package sets `selinux: true` and ships `k3s-selinux-*.rpm` under `selector.package: binary`. The BIN upload phase moves staged files into place and runs no package manager, so that RPM lands in `/var/lib/rancher/k3s/rpm/` and the `%post` that loads the policy module never runs -- leaving a node with SELinux enforcing, an engine told to use it, and no policy for it. The `rke2` examples select `package: rpm` for the equivalent file, which is why `semodule -l` lists `rke2` there.

So a k3s walk on this fleet is worth trying and is not yet known to pass. `mage test:endToEndClusterVM` still defaults to rke2 for that reason; `CARGOSHIP_E2E_DISTRO=k3s` is how to find out, and the node's journal is where the answer will be.

## Limits

The fleet runs one distribution. That is enough to exercise the Enterprise Linux branch against something that really has SELinux, fapolicyd and firewalld, and it is not a substitute for the bootloose cluster's mix of Ubuntu, Fedora and Alpine nodes - apply routes on OS family in several places, and a single-family fleet never takes the branches that do not match. Run both.
