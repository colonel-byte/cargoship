# Setting a machine up to run the VM fleet

[`mage dev:vmUp`](microvm.md) runs its virtual machines under plain qemu, as your own user, with no daemon and no root. This page is how to get a machine into that state and how to tell that it worked. [choice-microvm-backend](../agent/choice-microvm-backend.md) records why the fleet is built this way rather than on libvirt or Firecracker.

Nothing here installs a service, adds a system unit, or changes a firewall, SELinux or kernel setting. That is deliberate: the fleet exists to test host configuration, so a developer running it should not have to change the configuration of the host they are running it from.

## What has to be present

Five executables and one device:

| Needed | Fedora package | Debian/Ubuntu package |
| ---------------------- | ----------------------- | ------------------- |
| `qemu-system-x86_64`   | `qemu-system-x86-core`  | `qemu-system-x86`   |
| `qemu-img`             | `qemu-img`              | `qemu-utils`        |
| `mkfs.vfat`            | `dosfstools`            | `dosfstools`        |
| `mcopy`                | `mtools`                | `mtools`            |
| `ssh`, `ssh-keygen`    | `openssh`               | `openssh-client`    |
| `/dev/kvm`             | the kernel              | the kernel          |

```sh
sudo dnf install qemu-system-x86-core qemu-img dosfstools mtools openssh
```

`guestfish`, from `libguestfs`/`guestfs-tools`, is optional. It is used once per image download to check that the base image carries cloud-init and sets SELinux to enforcing; without it the fetch says it is skipping that check and carries on.

`dev:vmUp` reports every missing tool in one message rather than failing part way through a bring-up, so running it is a reasonable way to find out what you are missing.

### Why no libvirt

There is no `libvirtd` here, and installing one changes nothing. qemu is driven directly, each node is a `qemu-system-x86_64` process owned by your user, and the only state is a directory under `build/microvm`. If you already run libvirt for other work it will not conflict: the fleet defines no libvirt domain, claims no libvirt network, and creates no tap device or bridge.

## KVM access, without being in a group

The fleet refuses to start without `/dev/kvm`, because the alternative is qemu falling back to emulation, which is slow enough that an engine never finishes coming up.

On current Fedora the device is world-accessible and there is nothing to do:

```sh
$ ls -l /dev/kvm
crw-rw-rw-. 1 root kvm 10, 232 Oct 10 15:44 /dev/kvm
```

That `crw-rw-rw-` is the point -- mode `0666`, set by a udev rule systemd ships, so an ordinary user can open it without being in the `kvm` group. On a distribution that still ships `0660 root:kvm`, the group is how you get access:

```sh
sudo usermod -aG kvm "$USER"   # then log out and back in, or: newgrp kvm
```

Check that virtualisation is actually available to the CPU, not merely that the device file exists:

```sh
grep -Eom1 'vmx|svm' /proc/cpuinfo   # vmx on Intel, svm on AMD; no output means it is off
```

No output usually means virtualisation is disabled in firmware rather than absent from the processor.

### If your development machine is itself a virtual machine

The fleet then needs nested virtualisation, which the outer hypervisor has to expose:

```sh
cat /sys/module/kvm_amd/parameters/nested     # or kvm_intel; 1 or Y means available
```

If that reads `0` or `N`, enable it on the host running your development machine -- on a Linux host, `kvm_intel.nested=1` or `kvm_amd.nested=1` -- rather than on the machine you are reading it from. Without it `/dev/kvm` may not exist at all inside the guest.

## Confirming a node really is accelerated

The surest check is how long a bring-up takes. A node reaches sshd in about six seconds with KVM; under emulation it is minutes, and that is the symptom people usually notice first.

```sh
mage dev:vmUp -control=1 -worker=0
```

To see it directly, look at what the process was given: the command line carries `accel=kvm`, and the fleet passes nothing else, so qemu either accelerates or fails to start.

```sh
pgrep -a qemu-system-x86_64 | tr ' ' '\n' | grep -A1 machine
```

## Memory, disk, and what a fleet costs

Each node defaults to 4GiB and 2 vCPUs, so the usual three-node fleet wants 12GiB available. `dev:vmUp` refuses a fleet that would not fit in `MemAvailable` rather than letting the OOM killer choose which process dies, and says what it would have needed.

Disk is cheaper than it looks. The base image is fetched once, about 520MiB, into `$XDG_CACHE_HOME/cargoship/microvm` -- outside the repository, so it survives a clean checkout. Each node is a copy-on-write overlay on top of it, a few megabytes at bring-up, growing to roughly 250MiB after a boot and one `dnf` transaction. Fleet state lives in `build/microvm`, which is gitignored and on real storage rather than a tmpfs; see [microvm](microvm.md) for why that matters here.

## Networking, and why there is nothing to configure

Neither interface a node gets needs a privilege or a host-side rule.

The management interface is qemu's `user` (slirp) network, implemented inside the qemu process. It provides the forwarded SSH port your machine connects through and outbound access for `dnf`. Nothing listens on a routable address: every forward binds `127.0.0.1`.

The fleet's own segment is a `socket` netdev on a multicast group, which joins the fleet's qemu processes into one broadcast domain over loopback UDP. There is no tap device, no bridge, and nothing for `firewalld`, `nftables` or Docker's rules to have an opinion about -- which is exactly why it was chosen. A fleet's internal networking cannot be broken by the host firewall configuration you are in the middle of changing.

Two consequences worth knowing before debugging: your machine cannot reach that segment at all, and the host's own firewall needs no rules for any of this. The first is covered under "What the management node can and cannot reach" in [microvm](microvm.md).

## Host SELinux

A host running SELinux in enforcing mode needs no change. The qemu processes are started from your shell, so they inherit your unconfined context rather than the `svirt_t` confinement libvirt would apply, and every file they touch -- the image cache, the overlays, the seed images -- is one your user owns. The fleet never relabels anything on the host.

Note that the *guests* run SELinux enforcing, and that is the point of the fleet rather than an accident of the host.

## When it does not work

| Symptom | Cause |
| ------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `/dev/kvm is not available`                             | The device is missing or not readable by you. See the KVM section above |
| `missing qemu-system-x86_64, mkfs.vfat` and similar     | Install the packages in the table above; the message names everything it could not find |
| `Could not access KVM kernel module` from qemu          | `/dev/kvm` exists but your user cannot open it -- check the mode and the `kvm` group |
| A node never answers ssh, minutes in                    | Usually emulation rather than KVM. The failure prints the tail of the node's serial console, and the full log is at `build/microvm/<fleet>/<node>/console.log` |
| `a 3 node fleet at 4096MiB each needs ...`              | The memory guard. Lower `-worker`, or pass fewer nodes |
| `cached image ... has digest ..., want ...`             | An interrupted or corrupted download. Remove the named file and run `mage dev:vmImage` |
