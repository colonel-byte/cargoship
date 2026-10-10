# Running the cluster suite against your own fleet

The cluster phase suite normally provisions the hosts it tests: bootloose containers by default, or local virtual machines with `CARGOSHIP_E2E_BACKEND=microvm`. The third backend provisions nothing. Point it at a `ZarfCluster` document naming hosts that already exist and it runs the same phase assertions against them.

```sh
CARGOSHIP_E2E_BACKEND=inventory \
CARGOSHIP_E2E_INVENTORY=$PWD/my-fleet.yaml \
  go test -mod=vendor -count=1 -v -timeout=90m ./test/e2e/cluster/...
```

That is what makes the suite usable as an acceptance test for an environment cargoship has not been proven against -- EC2, GCE, vSphere, Proxmox, Hetzner, bare metal -- rather than only as a regression test for the two fleets this repository knows how to build. The phases are the product; the hosts are yours.

**The apply walk expects hosts with no engine on them.** `Test_12_GatherFactsDistro` asserts every host reports no engine version before anything is installed, which is what makes the later upgrade-phase assertions mean anything. So the walk is a one-shot against a given fleet: running it a second time fails, correctly, and what follows an install is the upgrade or reset walk rather than another apply. Reset or rebuild the hosts before installing again.

**It changes the hosts it is given, and it does not change them back.** The walk installs an engine, rewrites `/etc/hosts`, configures the firewall, writes sysctl and fapolicyd files, and installs packages. Nothing is torn down on the way out -- the suite did not create these machines, so it will not destroy them, and that cuts both ways. Use hosts you are willing to lose.

## The document

The same `ZarfCluster` document `cargoship apply --config` takes. It is read through the loader the CLI uses, so anything cargoship accepts is accepted here and anything it rejects fails before a host is touched.

```yaml
apiVersion: zarf.dev/v1alpha1
kind: ZarfCluster
metadata:
  name: my-fleet
spec:
  config:
    loadbalancer: 10.0.1.10
  hosts:
    - ssh:
        address: 10.0.1.10
        user: ubuntu
        port: 22
        keyPath: /home/me/.ssh/fleet
      hostname: ip-10-0-1-10
      privateAddress: 10.0.1.10
      role: controller
      profile: controller
    - ssh:
        address: 10.0.1.11
        user: ubuntu
        port: 22
        keyPath: /home/me/.ssh/fleet
      hostname: ip-10-0-1-11
      privateAddress: 10.0.1.11
      role: worker
      profile: worker
```

[`test/e2e/cluster/testdata/inventory-external.yaml`](https://github.com/colonel-byte/cargoship/blob/main/test/e2e/cluster/testdata/inventory-external.yaml) is a working example of the shape, and is what the unit tests read.

What the suite requires of it:

*   **At least one host with `role: controller`.** Without one there is no control plane to install, and the run says so rather than failing inside a phase.
*   **`role` on every host.** This backend reads roles from the document and never from the hostnames. The other two fleets name their machines `kc0`, `kw0` and so on and let a prefix carry the role; your hostnames are whatever your fleet calls them, so the field is the only source.
*   **`privateAddress` on every host, and `privateInterface` where the default route is not on the interface the nodes talk to each other over.** The firewall and `/etc/hosts` phases distribute these to every peer. Fact gathering will discover an address if you leave it out, and on a multi-homed host it discovers the one holding the default route, which may not be the one the cluster should use.
*   **Key-based SSH, as a user who can `sudo` without a password.** Every phase runs privileged commands through `Sudo()`.
*   **`SSH_KNOWN_HOSTS=""`** in the environment if your hosts' keys are not in a known-hosts file the test binary will read.

## Environment

| Variable | What it does |
| ----------------------------- | ----------------------------------------------------------------------------------------------- |
| `CARGOSHIP_E2E_BACKEND`       | `inventory` to select this backend |
| `CARGOSHIP_E2E_INVENTORY`     | Path to the `ZarfCluster` document |
| `CARGOSHIP_E2E_JOIN_HOST`     | Hostname the join walk should join. Unset skips that walk; see below |
| `CARGOSHIP_E2E_DISTRO`        | `k3s` (default) or `rke2`, which engine to install |
| `SSH_KNOWN_HOSTS`             | Set to `""` when your hosts' keys are not in a known-hosts file the test binary reads |

## What runs, and what stands down

Every phase runs. Some assertions do not, because they are assertions about a fleet this suite chose:

| Assertion | Against your fleet |
| ------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Each host runs the OS its image declared     | Stands down. Your hosts' operating systems are not knowable until phase 09 has asked them, so the test asserts only that detection produced an answer, and logs the mix it found |
| The fleet runs every OS family               | Stands down, for the same reason |
| An Enterprise Linux host exists for the RPM phase, a Debian host for the APT phase | Stands down. Those guards protect a fleet that was meant to cover a branch and lost the host for it, which cannot be said about hosts the suite did not choose |
| The join walk                                | Skipped unless `CARGOSHIP_E2E_JOIN_HOST` names a host for it, because the suite cannot create one. See "Adding a host" below |
| Everything else -- the prepare phases, SELinux, fapolicyd, the firewall, all four upload phases, the engine config, the engine install, the upgrade and reset walks | Runs, and asserts |

The run logs which of these it stood down, so a green result does not quietly read as broader coverage than it was.

## Adding a host, and running the join walk against it

The join walk installs the engine on a host that was not in the cluster when the install ran, then asserts the cluster is one node larger rather than one node different. The other two backends create that host on demand. Here you create it, and it takes two runs -- the suite's walks run in one process, so there is no moment in a single run at which a host can appear.

**First run: install the cluster on the hosts you have.** Stop after the apply walk, because the join walk has nothing to join yet. The hosts have to be carrying no engine at this point, per the note above.

```sh
CARGOSHIP_E2E_BACKEND=inventory CARGOSHIP_E2E_INVENTORY=$PWD/my-fleet.yaml   go test -mod=vendor -count=1 -v -timeout=90m -run 'TestClusterPhases/apply' ./test/e2e/cluster/...
```

**Then provision the new host yourself** -- another instance in the same subnet or VLAN, reachable over SSH by the same key, able to reach the other nodes on the ports the engine uses -- and add it to the document. A worker, since that is what the walk joins:

```yaml
    - ssh:
        address: 10.0.1.12
        user: ubuntu
        port: 22
        keyPath: /home/me/.ssh/fleet
      hostname: ip-10-0-1-12
      privateAddress: 10.0.1.12
      role: worker
      profile: worker
```

**Second run: name it, and run only the join walk.**

```sh
CARGOSHIP_E2E_BACKEND=inventory CARGOSHIP_E2E_INVENTORY=$PWD/my-fleet.yaml CARGOSHIP_E2E_JOIN_HOST=ip-10-0-1-12   go test -mod=vendor -count=1 -v -timeout=60m -run 'TestClusterPhases/join' ./test/e2e/cluster/...
```

`CARGOSHIP_E2E_JOIN_HOST` is the hostname as the document spells it, not the address. The walk resolves the host out of the inventory by that name, so a mismatch fails before any phase runs rather than after half a walk.

### What that does with the document

The named host is held out of the fleet the install is considered to have produced, and put back for the join. One file, read twice: the apply walk's view has every host except that one, the join walk's view has all of them, and the host counts the assertions compare against come out exactly as they do for a fleet the suite provisioned. That is why the new host must be in the document for the *second* run and may be absent from the first -- adding it earlier is harmless, because the first run does not look at it either way.

The walk then asserts what it always asserts: the phases claim the new node and not the established ones, the engine comes up on it at the version the cluster already runs, every established node's firewall learns the new node's address, and the cluster reports one more `Ready` node than before.

Two ways naming it goes wrong, both reported before a host is touched:

*   **A name the document does not hold.** Check the `hostname` field rather than the address.
*   **The only host in the document.** There is no cluster for it to join.

Leave `CARGOSHIP_E2E_JOIN_HOST` unset and the join walk is skipped with a line saying why, which is the right default: a document whose hosts are all already in the cluster has nothing to join.

## Removing a host

Removing one node is `cargoship reset` scoped to it. Keep the whole fleet in the document and name the host:

```sh
cargoship reset --config my-fleet.yaml --distro rke2 --confirm --target-hosts ip-10-0-1-12
```

The full inventory matters. The node has to be drained and deleted from the cluster before its engine comes down, and cargoship does that by driving `kubectl` through a controller that is *staying* -- so there has to be one in the document. A run that targets every running controller while keeping some other host is refused rather than attempted, because it would delete nodes through a machine it is about to uninstall. Targeting every host is allowed: that is a whole-cluster reset, which is what plain `reset` is.

Check it with `--dry-run` first. It reports every host it would reset and what it would do to each, and touches none.

> `--target-hosts` arrives with [#646](https://github.com/colonel-byte/cargoship/pull/646) and is not on `main` yet. Without it `reset` acts on every host the document names, and there is no good way to remove one: an inventory holding only that host uninstalls its engine but has no controller to delete the node through, so the cluster keeps a stale `NotReady` node object, and an inventory holding the host and a controller resets the controller too.

**Then take it out of the document**, and check what is left. Not with the apply walk: the remaining hosts run the engine, and that walk asserts they do not. A dry run of the CLI reports over live state and changes nothing, so it is the right tool here:

```sh
cargoship apply --config my-fleet.yaml --dry-run <package>
```

It should name no install work for the remaining hosts -- they are already at the packaged version -- and no host you removed. Confirm the cluster agrees from inside a node, which is also the only place the API is reachable on a fleet whose load balancer is private:

```sh
kubectl get nodes      # the removed node should be gone, not NotReady
```

A node deleted from the cluster but left in the document shows up as a host the dry run still plans for, which is the point of doing it in that order.

Removing the *whole* cluster is the reset walk, which runs last in a full `TestClusterPhases` and needs no flags. It is destructive and backs nothing up; what follows a reset is an apply.

## Reading a failure

A failure here is more likely to be about your environment than about cargoship, and the suite is not in a position to tell the difference. Three things worth checking before filing anything:

*   **Node-to-node traffic.** The engine phases are the first that need it, and a security group or host firewall that allows your SSH but not the cluster's own ports fails at the worker join -- the agent never starts, and the real reason is in the node's journal rather than in the test output. The phase captures the journal of the host whose wait timed out.
*   **Which address the nodes got.** If `privateAddress` is wrong or discovered from the wrong interface, the firewall and `/etc/hosts` phases distribute addresses no peer can reach, and everything up to the engine still passes.
*   **`dnf versionlock` and the SELinux policy packages.** A minimal Enterprise Linux image carries neither the versionlock plugin nor `container-selinux`, and phase 21 is fatal when it cannot install the latter. Both need a reachable package repository, which an air-gapped fleet does not have by definition -- stage them into the image instead.

## Verifying work that is not on `main` yet

This backend and [the VM fleet](microvm.md) are also how a change to the lifecycle phases gets exercised before it merges, which previously had no local path at all: the container backend cannot form a multi-node cluster on a host whose firewall drops container-to-container traffic, so the full walk -- the join, upgrade and reset walks, and every phase after the engine starts -- has never run outside CI, and CI runs only the staging half.

Check out the branch, bring a fleet up, and run the suite against it. `--target-hosts` above is the worked example: a subset reset is a change whose whole risk is in which hosts it claims and which controller it drives `kubectl` through, and neither is observable without a real multi-node cluster to take one node out of.

## The other two backends

[microvm](microvm.md) is the local VM fleet, which is the one to reach for first: it is a real kernel with SELinux enforcing and costs a minute to bring up. [e2e-tests](e2e-tests.md) and [e2e-phase-tests](e2e-phase-tests.md) cover the suite itself and the default container backend.
