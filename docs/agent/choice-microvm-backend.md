# Why the local VM fleet runs on plain qemu, and what it does not replace

`internal/microvm` brings Enterprise Linux virtual machines up and down on a developer's own machine so that the phases routing on OS family can be exercised against a real kernel. It exists because a container cannot answer the question `pkg/phase/21_prepare_selinux.go` asks: that phase gates on `getenforce` reporting `Enforcing`, no container reports it, and the phase is fatal on failure -- so the one branch able to stop an apply was the one branch never run. `pkg/phase/22_prepare_fapolicyd.go`, the firewalld backend in `pkg/firewall/firewalld.go`, the `semodule` scriptlets in `k3s-selinux` and `rke2-selinux`, and the reboot in `pkg/phase/23_prepare_kernel_modules.go` are all in the same position, and `selinux: true` is set by every shipped package under `example/` without anything ever having checked that an engine configured that way starts.

## qemu, not Firecracker, and not libvirt

Firecracker is the obvious reach for "microVM", and it does not fit. It requires an uncompressed kernel binary with every driver statically compiled and no bootloader, and explicitly does not support loadable modules -- which is exactly what k3s and rke2 need, and what `23_prepare_kernel_modules` writes `/etc/modules-load.d` entries for. A RHEL-family `GenericCloud` qcow2 cannot boot on it at all without extracting a kernel out of the image first. qemu's own `microvm` machine type carries the same constraint for the same reason, so the name is not the thing that matters here.

libvirt would work, and costs more than it returns. It needs `libvirt-daemon-kvm` and `virt-install` installed, a daemon running, and its NAT network managed through the host's firewalld -- host state, on a machine whose SELinux and firewall configuration is frequently the thing under investigation. Lima was ruled out on a narrower point: its default 9p mounts are documented as incompatible with AlmaLinux, CentOS Stream, Oracle Linux and Rocky.

Plain qemu needs nothing beyond `qemu-system-x86_64`, `qemu-img`, `mkfs.vfat`, `mcopy` and `ssh`, runs as the developer, and leaves nothing behind. Each node is a copy-on-write overlay over one cached base image, so a bring-up costs a few megabytes and the overlay is recreated every time -- which is also why `Up` refuses a live fleet rather than adopting it. A node carrying state from a previous run is the single thing that makes a phase failure impossible to reason about.

## A multicast socket netdev, not a bridge

Every node gets two interfaces. The management one is a slirp `user` netdev with loopback forwards, which needs no privileges and gives the outbound access phase 21 depends on -- it installs `container-selinux` by name and is fatal if it cannot reach a mirror. The private one is `-netdev socket,mcast=`, which joins every qemu process in the fleet onto one broadcast domain.

A tap device or a bridge would be the conventional answer and both need root. The deciding point is subtler than that: on a host running firewalld with Docker's nftables firewall backend, container-to-container traffic is silently dropped, which is enough on its own to stop every multi-node run from forming a cluster. A bridge is subject to the same ruleset. A multicast socket between qemu processes over loopback is not something the host's firewall has an opinion about, so the fleet's own networking cannot be broken by the host configuration a developer is in the middle of changing. `vde_switch` would have been the other rootless option and is not packaged for current Fedora.

The private segment is also why `Inventory` sets `PrivateAddress` and `PrivateInterface` explicitly, where the bootloose equivalent leaves both to fact gathering. A node's default route is on the management interface, so discovery would give every host the same slirp address -- `10.0.2.15`, reachable from no peer -- and the firewall and `/etc/hosts` phases would faithfully distribute it.

## Infra nodes are workers with a profile of their own

A fleet is counted in three groups -- control, worker, infra -- and the third is not a third role. The engine has two, so an infra node joins as a worker and is distinguished only by its `Profile`, which `pkg/phase/81_label_nodes.go` turns into a `node-role.kubernetes.io/infra` label and which a cluster config can hang a per-profile `Concurrency` off.

They exist because every other host in a generated inventory has a profile that restates its role, which makes both of those mechanisms untestable: a label derived from `controller` on a controller proves nothing about a label derived from a name the role does not supply, and a per-profile concurrency is indistinguishable from the phase default when every profile holds the same hosts as its role. One group whose profile is not its role's name is enough to tell those apart.

They are named `ki*`, a prefix of its own rather than an extension of the worker prefix the way the container fleet's `kwf*` and `kwa*` are. The three prefixes then partition the names, so nothing matching on them depends on test order or on matching longest -- which the container suite's prefixes do, since `kwa` is also `kw`.

The consequence is that a mapping keyed only on `kc` and `kw` claims an infra node as neither. Nothing in `internal/microvm` is such a mapping: `Inventory` reads a node's role and profile off the node, not off its name. The container suite's `renderClusterInventory` *is* one, so wiring infra nodes into that fleet means teaching it this prefix rather than relying on the name to carry the role.

The group is created last, after the plain workers, so that asking for infra nodes does not renumber the hosts ahead of them -- a fleet brought up with none is addressed exactly as it was before the group existed.

## `internal/`, not `magefiles/pkg/`

[`magefiles/AGENTS.md`](../../magefiles/AGENTS.md) puts implementation logic under `magefiles/pkg/`, and this package is deliberately not there. It has two consumers -- the `Dev` mage targets and the e2e cluster suite -- and a test suite importing out of `magefiles/` would invert the dependency. `internal/` is what the root [`CLAUDE.md`](../../CLAUDE.md) names for packages private to the module, and the mage targets stay thin wrappers over it, which is what that rule is protecting.

Fleet state lives under `build/microvm/` rather than `TMPDIR`. An overlay reaches a quarter of a gigabyte after one boot and one dnf transaction, and `TMPDIR` is routinely a tmpfs here -- the repository's own `.envrc` points it at one. `build/` is already the scratch directory, gitignored and on real storage. Note that `dev:clean` does not touch it -- that target removes built binaries, and removing a live fleet's directory out from under it would orphan the qemu processes holding its overlays. `dev:vmDown` is what takes a fleet away.

## Rocky only, and why that does not retire the bootloose cluster

The fleet runs one distribution. [`choice-phase-e2e-tests`](choice-phase-e2e-tests.md) argues that the OS *mix* is the substantive part of the bootloose cluster -- apply routes on OS family in at least five places, and on a single-family cluster the branch that does not match is never taken while the gate assertions still pass, so the suite reports coverage it does not have. That argument applies to this fleet exactly as written.

So the VM backend is additional coverage, not a replacement. It is the only way to run the Enterprise Linux branch against something that actually has SELinux, fapolicyd and firewalld; the bootloose matrix remains the only thing that covers routing between families, and the Alpine upload-only node remains the only host belonging to neither. `CARGOSHIP_E2E_BACKEND` selects between them and defaults to bootloose.

Adding a second guest image is a later change and a cheap one -- the image is one pinned URL and digest in `internal/microvm/image.go`. It was left out of the first version because one distribution with SELinux enforcing closes the gap that motivated the package, and a second would double the bring-up cost of every run before anything had been learned from the first.

## Two qemu details that each cost a boot to find

`-display none`, not `-nographic`: qemu refuses `-nographic` together with `-daemonize`, and reports it only once the process has been launched, so the failure arrives as a node that never appears rather than as a bad flag. And the overlay is opened `cache=unsafe`, which is correct precisely because these disks are disposable -- there is no state on them worth an fsync, and the next bring-up recreates them.
