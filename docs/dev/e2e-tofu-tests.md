# Running the OpenTofu Provider End-to-End Tests

`test/e2e/tofu/` is the only suite that drives cargoship the way an OpenTofu practitioner does: it runs the real `tofu` binary over the module in `example/terragrunt/terraform/modules/cluster`, which loads the provider out of a local filesystem mirror, which starts the phases in a process the test does not control.

That layer is what has no coverage anywhere else. The [phase suite](e2e-phase-tests.md) calls the phases in-process, and the provider's unit tests use a fake converger, so everything between a configuration and the phases is untested by both: schema decoding, the map-keyed `hosts` set, the computed attributes a plan has to be able to promise, state, the `state = "absent"` removal, and a destroy.

## What it provisions

Three bootloose machines on one image - `tkc0`, a controller, and `tkw0`/`tkw1`, workers - each privileged, each with a Docker volume over `/var/lib/rancher`. The reasons are the ones [e2e-phase-tests](e2e-phase-tests.md) sets out for the larger cluster: the engine loads modules and runs its own containerd, and overlayfs cannot be stacked on itself.

One image rather than three, because nothing in this suite routes on the OS family; that is the phase suite's job, and repeating it here would cost a second and third engine install for assertions already held. Three machines rather than one, because a removal needs a worker to remove and a controller that stays to drive it.

Cargoship does not load those modules itself, which is worth knowing because it is not an oversight in the suite: `PrepareKernelModules` belongs to the `prepare` action rather than to `apply`, it is gated on the package declaring an `os.kernel` list, and no shipped definition declares one. What a node's kernel carries is the machine image's business -- and here the machine image is a bootloose container, so it is this suite's. Without the legacy `iptable_nat` table, kube-proxy exits and the engine shuts itself down a few seconds after reporting ready, which systemd then repeats for the whole phase timeout.

The package is built from `example/k3s-flannel/v1_36/v1.36.4-k3s1` through `test.ContainerSafeDefinition`, which is the shipped definition with the two `net.netfilter` sysctls a container cannot apply removed. k3s rather than rke2 for the reason the phase suite's full walk is k3s: a single controller means a SQLite datastore and no etcd quorum to time out under the contention of three nested node containers.

## Prerequisites

*   **Docker**, for the machines.
*   **`tofu`** on `PATH`. The suite skips without it rather than failing - a machine that has no OpenTofu was never set up to run this. `CARGOSHIP_E2E_TOFU_BINARY` names it differently if yours is installed under another name.
*   **container-to-container networking**. A worker joins by reaching the controller's supervisor, so a host that forwards no traffic between containers cannot run a cluster -- and host-to-container still works in that situation, so SSH and the controller's own install succeed and only the worker join fails. The suite probes it with two throwaway containers before provisioning anything. The usual cause is a host firewall: firewalld with Docker's nftables backend drops forwarded traffic between containers even with `enable_icc=true` on the bridge, and a user-defined network does not escape it. Either put the bridge in firewalld's docker zone, or put Docker back on the iptables firewall backend with `{"firewall-backend": "iptables"}` in `/etc/docker/daemon.json` -- which also loads the modules below, making this host's two problems one fix.
*   **the kernel modules the engine needs**: `br_netfilter`, `ip_tables`, `iptable_filter`, `iptable_nat`, `overlay` and `vxlan`. The suite checks `/proc/modules` and loads only what is missing, from a throwaway privileged container -- the node image carries no `kmod` at all, so it cannot load them itself. Set `CARGOSHIP_E2E_SKIP_MODPROBE=1` to leave the kernel alone; the check still runs and names what to load.
*   **the provider in the local mirror**, at `build/tofu-provider/mirror/registry.opentofu.org/colonel-byte/cargoship/0.1.0/<os>_<arch>/`. The suite skips if it is not there, and the skip names the target that builds it.

The version is `0.1.0` rather than `0.0.0` because OpenTofu refuses `0.0.0` - it is reserved for a provider that is not published - and the module's `required_providers` asks for `>=0.1.0`. See [tofu-provider](tofu-provider.md) for the development loop this shares its mirror with.

## Running

```console
$ mage test:endToEndTofu                    # builds the mirror, removes stale bootloose containers, runs the suite
$ go run ./magefiles/core test:endToEndTofu # same, with no mage binary installed
```

The mage target is the normal way in because it builds the mirror first. A plain `go test` works once the mirror exists:

```console
$ mage release:tofuProviderDev 0.1.0
$ go test -mod=vendor -count=1 -v -timeout=90m ./test/e2e/tofu/...
```

Expect tens of minutes: the walk is four real applies, each one an engine install, convergence, removal or teardown over SSH.

## The walk

One test with ordered subtests, because every step starts from what the one before it left on the hosts and in state. A step that fails stops the walk rather than asserting against a cluster that was never built, and the destroy is registered with `t.Cleanup` before the first apply, so a failure anywhere still takes the engine off the machines.

| Step         | What it holds                                                                                                                                                                |
| :----------- | :--------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `init`       | The provider resolves with no registry, out of the filesystem mirror the generated CLI configuration points at                                                               |
| `plan`       | The plan holds a `cargoship_cluster` and creates it - a plan that quietly plans nothing because the module took no hosts is not otherwise visible                            |
| `apply`      | The engine in state is the one the package carried, every host is unmarked, and the controller's own `kubectl` reports three nodes                                           |
| `kubeconfig` | The credentials `export_kubeconfig` returned reach the cluster: every node Ready through them                                                                                |
| `converge`   | A second plan over an unchanged configuration reports no changes, which is also where a computed attribute that cannot be promised would show up                             |
| `remove`     | `state = "absent"` on one worker drains, deletes and uninstalls it in one apply; `removed` is true for it and false for the rest, the node is gone and its engine is stopped |
| `destroy`    | State is emptied and the controller's engine is no longer running                                                                                                            |

The node counts come from the controller's own `kubectl` rather than from the provider's `nodes` attribute. The attribute is what the provider believes, and a provider that believed the wrong thing is the failure worth catching.

## Why the root module is a fixture and the fleet is not

`test/e2e/tofu/testdata/main.tf` is the root module, copied into a temp directory beside a symlink named `module` that points at the shipped module. The fleet, the package path and the per-step settings arrive as a generated `terraform.tfvars.json`.

The split follows [`test/e2e/AGENTS.md`](https://github.com/colonel-byte/cargoship/blob/main/test/e2e/AGENTS.md): the HCL is a document a reviewer reads as HCL, and the only part that varies per run is data. The module arrives as a symlink rather than a copy because a local module source resolves relative to the file naming it, so a copied root module could not reach back into the repository without a path that depends on where the temp directory landed.

It consumes the shipped module rather than declaring the resource itself, which buys one thing a bare resource would not: the module's variable types have to keep accepting what the provider's schema wants. A `hosts` object type that no longer converts is a failure nothing else here would see.

## Environment variables

*   **`CARGOSHIP_E2E_TOFU_BINARY`** - the binary to run instead of `tofu`.
*   **`CARGOSHIP_E2E_SKIP_MODPROBE`** - do not load kernel modules. The check that they are loaded is not turned off with it, so a missing module fails the run naming `sudo modprobe ...` rather than being discovered twenty minutes later in a journal. Worth setting on a machine whose kernel is not a test's to change.
*   **`CARGOSHIP_E2E_TOFU_PHASE_TIMEOUT`** - how long a phase waits on a host, as a Go duration (default `20m`). Lower it when investigating a failure: a phase that is going to fail waits out the whole timeout first, so the default costs twenty minutes to arrive at the same error `6m` reaches in six.
*   **`CARGOSHIP_E2E_KEEP_CLUSTER`** - leave the machines running when the run failed. The same variable the phase suite reads, for the same reason: a failed engine install is explained by the journals in the node containers, and deleting the containers takes them with it. Remove them afterwards with `mage test:cleanCluster`.
*   **`CARGOSHIP_E2E_TMPDIR`** / **`TMPDIR`** - where the package build and the OpenTofu working directory are staged. `mage test:endToEndTofu` points both at `build/tmp`.

## The two preflights, and why they exist

Both failures they catch were found the expensive way, and both look like something else while they happen.

A missing kernel module lets the engine start, notify readiness, and shut itself down a few seconds later -- which systemd repeats for the whole phase timeout, so the symptom is twenty minutes of restarts and no error from cargoship. Blocked inter-container traffic lets everything up to the worker join succeed, so the symptom is one worker timing out with `service k3s-agent is not running` and nothing to say why.

Neither is cargoship's to fix: the first is what the machine image carries, the second is the host's firewall. What the suite owes a contributor is to say so in seconds rather than after a phase timeout, which is what `requireKernelModules` and `requireContainerNetworking` are for. They run before the machines, so a host that cannot run the suite starts no container.

## Debugging a failure

Run with `-v`. Every `tofu` invocation is logged with its combined output through `t.Log`, so the provider's own diagnostics and the phase logs it routes through `tflog` are in the test output rather than somewhere else.

A failure during an apply leaves the machines up for as long as the process lives, and the destroy in `t.Cleanup` then tears the cluster down. To keep the machines, set `CARGOSHIP_E2E_KEEP_CLUSTER=1` and read them with `docker exec`:

```console
$ docker exec cargoship-e2e-tofu-tkc0 journalctl -u k3s-server --no-pager | tail -50
$ docker exec cargoship-e2e-tofu-tkw1 systemctl status k3s-agent
```

The container name is the cluster name and the machine name, not the hostname -- `docker ps --filter label=io.k0sproject.bootloose.owner=bootloose` lists them.

The OpenTofu working directory is a `t.TempDir()`, which `go test` removes at the end of the test - including the state file. A failure that needs the state inspected is the case to copy it out from inside the test rather than to go looking for it afterwards.

## What it does not cover

*   **The data source.** `cargoship_cluster_facts` reads a fleet and is covered by the provider's unit tests only; it still takes `host` blocks rather than a map, which this suite never exercises.
*   **Terragrunt.** The suite runs `tofu` directly. The terragrunt layout in `example/terragrunt` is checked by `terragrunt validate` by hand; nothing automated reads `terragrunt.hcl`.
*   **A controller removal.** The removal step removes a worker. Removing a controller is the dangerous case - it gives up an etcd member - and the rules around it are unit-tested and documented rather than proven here. See [`docs/agent/choice-tofu-host-removal.md`](https://github.com/colonel-byte/cargoship/blob/main/docs/agent/choice-tofu-host-removal.md).
*   **An OCI mirror.** The provider resolves out of a `filesystem_mirror`. The `oci_mirror` path a release publishes to is checked by reading the artifact with the ORAS CLI, not by an init through it.
