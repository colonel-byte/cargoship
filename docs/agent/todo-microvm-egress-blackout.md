# TODO: cut the microvm fleet's internet access once its nodes are up

This is a punch list, not a decision record like the `choice-*.md` files beside it. Cargoship's whole premise is that a target host receives everything over SSH from a staged package and reaches nothing itself, and no backend currently proves that: a phase that quietly fetched something from the internet would pass on every one of them. The microvm fleet has working egress today, measured from `kc0` mid-run - `github.com` resolves and `curl https://github.com` returns `200`. This document is what it would take to take that away, and what not to try.

## Why it is not one command

Each node gets two NICs from `test/microvm/vm.go`:

| NIC       | QEMU netdev                                 | Address          | Carries                                              |
| --------- | ------------------------------------------- | ---------------- | ---------------------------------------------------- |
| `enp0s3`  | `-netdev user,id=mgmt,hostfwd=...` (SLIRP)  | `10.0.2.15/24`   | egress **and** the inbound SSH every phase runs over |
| `enp0s4`  | `-netdev socket,id=lan,mcast=...`           | `10.73.193.x/24` | the fleet's own layer 2 segment, no egress           |

So the mgmt NIC cannot simply be dropped - that is the SSH. The detail that makes a blackout possible is that SSH arrives from `10.0.2.2`, the SLIRP gateway, which is reachable through the kernel's own `10.0.2.0/24 dev enp0s3 scope link` route rather than through the default route.

## The approach to take

Apply an nftables blackout inside each guest after cloud-init has finished, gated behind an environment variable so the default behaviour does not change:

```sh
nft add table inet airgap
nft add chain inet airgap out '{ type filter hook output priority 0; policy accept; }'
nft add rule inet airgap out oifname enp0s3 ct state established,related accept
nft add rule inet airgap out oifname enp0s3 drop
```

Inbound SSH survives on established/related, anything the guest initiates is dropped, and `enp0s4` and the CNI's interfaces are untouched. It belongs in the microvm provisioning path after the cloud-init wait, not in the suite, so that `mage dev:vmUp` can produce an air-gapped fleet to poke at by hand as well.

## Two things not to try

**`-netdev user,restrict=on` has the right semantics and the wrong timing.** It isolates the guest while leaving `hostfwd` working, which is exactly what is wanted, but it is a launch-time flag and cloud-init installs `firewalld`, `fapolicyd` and `python3-dnf-plugin-versionlock` with `dnf` (`test/microvm/image.go`). A node that boots restricted never provisions. Hot-swapping the netdev over QMP after bringup avoids that at the cost of a QMP socket and dropping the NIC's connections, which is more machinery than this is worth.

**`ip route del default` is half a blackout.** `/etc/resolv.conf` points at `10.0.2.3`, which is on-link on `10.0.2.0/24`, so name resolution keeps working after the default route is gone and only routed egress dies. A test that still resolves names is not testing an air gap.

## What to expect, and what this does not cover

- **The first run will not be green.** Any step quietly relying on node egress starts failing, which is the point, but budget for fallout rather than a clean pass.
- **firewalld is cargoship's to manage on these nodes.** A separate `inet airgap` table should be independent of firewalld's own and survive its reloads - that is the claim most worth testing first, because the phases reload firewalld during a walk.
- **Package assembly is not in scope and must stay out of it.** `distro.Create` runs on the machine running the suite, not on a node, and downloading the sources a `distro.yaml` names is what a build is for. A transient DNS failure there already failed an upgrade walk at `Test_00_UpgradePackage`; blacking out the nodes neither causes nor fixes that. Pre-warming the download cache is a separate idea.
