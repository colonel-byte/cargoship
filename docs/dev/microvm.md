# A local VM fleet

`mage dev:vmUp` brings up a fleet of Rocky 10 virtual machines on your own machine and writes a `ZarfCluster` inventory pointing at them, so that `cargoship prepare` and `cargoship apply` can be run against hosts with a real kernel. `mage dev:vmDown` takes it away again.

It exists for the phases the [cluster e2e suite](e2e-tests.md) cannot reach. Those nodes are containers, and a container reports no SELinux, runs no fapolicyd and has no firewall of its own, so [`pkg/phase/21_prepare_selinux.go`](https://github.com/colonel-byte/cargoship/blob/main/pkg/phase/21_prepare_selinux.go) and its neighbours correctly do nothing there. Testing a change to any of them means a virtual machine. [choice-microvm-backend](../agent/choice-microvm-backend.md) records why this is plain qemu rather than Firecracker or libvirt.

Nothing it does touches the host. It needs no root, starts no daemon, and changes no SELinux, firewall or kernel setting on your machine - which is the point, since the configuration under investigation is usually the host's own.

## What you need

`qemu-system-x86_64`, `qemu-img`, `mkfs.vfat`, `mcopy`, `ssh`, and a readable `/dev/kvm`. On Fedora those come from `qemu-system-x86`, `qemu-img`, `dosfstools`, `mtools` and `openssh-clients`. `guestfish`, from `guestfs-tools`, is optional: it verifies a freshly downloaded image, and the fetch says so and carries on when it is absent.

`mage dev:vmUp` reports every missing tool at once rather than failing part way through a bring-up.

## Bringing a fleet up

```sh
mage dev:vmUp 1 2     # one controller, two workers
```

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

`CARGOSHIP_VM_DISTRO=rke2 mage dev:vmUp 1 2` changes nothing about the guests. It decides only which of the leader's ports are forwarded to your machine, since rke2 serves its join endpoint on 9345 and k3s multiplexes onto 6443.

## Looking around, and tearing down

```sh
mage dev:vmList         # what is there and whether it is running
mage dev:vmShell kc0    # a root shell on one node
mage dev:vmDown         # every node stopped, every trace removed
mage dev:vmImage        # fetch and verify the base image, start nothing
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

## The engine's node IP, and why it reads wrong here

A node is multi-homed, and the engine picks its own node address from whichever interface holds the default route - which here is the management interface. So `kubectl get nodes -o wide` reports `INTERNAL-IP 10.0.2.15` for every node, the same slirp address on each.

This is worth knowing before it is mistaken for a fleet defect. Cargoship puts a host's `privateAddress` into the engine's `tls-san` and into the firewall's node ipset, but it does not render a `node-ip`, and the inventory has no per-host engine argument to set one with. On a single-homed fleet, which is what cargoship is normally pointed at, the engine's choice and the private address are the same thing and the gap is invisible. Here they are not.

An rke2 cluster still comes up `Ready` with it, so this does not stop the phases the fleet exists to test. Treat anything that depends on distinct node addresses - pod-to-pod traffic across nodes, anything reading a node's `InternalIP` - as out of scope until the engine config carries a node address.

## Limits

The fleet runs one distribution. That is enough to exercise the Enterprise Linux branch against something that really has SELinux, fapolicyd and firewalld, and it is not a substitute for the bootloose cluster's mix of Ubuntu, Fedora and Alpine nodes - apply routes on OS family in several places, and a single-family fleet never takes the branches that do not match. Run both.
