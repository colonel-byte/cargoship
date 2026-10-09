# How the OpenTofu provider is tested end to end

`test/e2e/tofu/` runs the real `tofu` binary over the shipped module against three bootloose machines. Several of its shapes look like arbitrary choices and are not, and two of its preflight checks look like paranoia until the failure they catch happens to you. This records both, plus what the suite has and has not actually proven.

## Why the real CLI rather than `terraform-plugin-testing`

The framework's acceptance harness is the obvious answer and was deliberately not taken. What it would add over this suite is per-step plan assertions; what it cannot add is the module, which is the layer a practitioner consumes. Driving `tofu` directly also means the suite exercises the pieces an acceptance test replaces with its own machinery: provider resolution out of a `filesystem_mirror` with no registry, the CLI configuration that keeps the run offline, and `tofu show -json` as the way state is read back.

The cost is that failures arrive as CLI output rather than as typed diagnostics, so the suite strips ANSI escapes from everything it captures and logs each invocation with its output. `NO_COLOR` and `TF_IN_AUTOMATION` do not turn the colour off, and `-no-color` is not accepted by every subcommand.

## Why it consumes the module and not a bare resource

A root module in `testdata/main.tf` calls `example/terragrunt/terraform/modules/cluster`. A bare `cargoship_cluster` resource would test the provider and nothing else; going through the module also holds that the module's variable types still accept what the provider's schema wants. That is the half which breaks silently: when `host` blocks became a `hosts` map, nothing but a real `tofu plan` would have shown whether `hosts = var.hosts` still converted against a `map(object({...optional...}))` carrying a computed `removed` attribute.

The module arrives as a **symlink** named `module`, not a copy. A local module source resolves relative to the file naming it, so a copied root module could not reach back into the repository without a path that depends on where the temp directory landed.

The HCL is a checked-in fixture and the fleet is generated `terraform.tfvars.json`, which is [`test/e2e/AGENTS.md`](https://github.com/colonel-byte/cargoship/blob/main/test/e2e/AGENTS.md)'s rule applied honestly: the configuration is a document a reviewer reads as HCL, and the only part that varies per run is data.

## Why three machines on one image, and k3s

One image because nothing in this suite routes on the OS family - that is the phase suite's job, machine by machine, and repeating it here would buy a second and third engine install for assertions already held elsewhere. Three machines because two of the three things the suite exists for need more than one node: a worker has to be removable while the cluster keeps running, and the controller driving that removal has to be a different machine from the one going.

k3s rather than rke2 for the reason the phase suite's full walk is k3s: a single controller means a SQLite datastore and no etcd quorum to time out under the CPU contention of three nested node containers.

## The two preflights

Both guard failures that were found the expensive way, and both masquerade as something else while they happen. They run before the machines are created, so a host that cannot run the suite starts no container.

**Kernel modules.** Without the legacy iptables tables, k3s starts, notifies readiness, and shuts itself down about three seconds later - `kube-proxy exited: iptables is not available on this host` - and systemd restarts it for the whole phase timeout. Seventy-seven restarts in eleven minutes, exit status 0 every time, and nothing from cargoship to say why; the explanation is only in the node's journal.

Cargoship does not load these itself, and that is not an oversight in the suite: `PrepareKernelModules` belongs to `action.NewPrepare` rather than to `NewApply`, it is gated on `opts.ModifyModules`, and it is gated again on the package declaring `spec.config.os.kernel`, which no shipped definition under `example/` does. What a node's kernel carries is the machine image's business - and a bootloose container's image is the suite's business. The provider has no prepare surface at all, so a fleet driven only through OpenTofu never runs those phases.

The loading happens from a throwaway privileged container with `/lib/modules` mounted, because the Ubuntu node image carries no kmod - no `modprobe`, no `insmod`. Modules belong to the host kernel, so one container loads them for every machine. `CARGOSHIP_E2E_SKIP_MODPROBE=1` turns the loading off and keeps the check, for a machine whose kernel is not a test's to change.

**Inter-container networking.** A worker joins by reaching the controller's supervisor, so a host that forwards no traffic between containers cannot run a cluster - and host-to-container still works in that situation, which is what makes it slow to find. SSH reaches the machines through published ports and the controller installs itself locally, so an apply succeeds all the way to the worker join, then spends a phase timeout on `service k3s-agent is not running`.

The usual cause is a host firewall rather than anything Docker was asked for: firewalld with Docker's nftables backend drops forwarded traffic between containers even with `enable_icc=true` on the bridge, and a user-defined network does not escape it. The probe is two throwaway containers and one TCP connection, and it names both fixes - the bridge into firewalld's docker zone, or `{"firewall-backend": "iptables"}` in `/etc/docker/daemon.json`. The second also makes Docker load the legacy iptables modules, which is why a host with this problem usually has both.

## One run at a time

The bootloose cluster name is fixed, as it is in the phase suite, so two runs of a suite share three container names. The loser is not the second run: the **first** run's `TestMain` teardown deletes the machines the second one is using, minutes after the second one started. That happened twice while this suite was being written, once because a `go test -short` did not skip the walk.

`-short` now skips, which closes the accidental route. Nothing prevents two deliberate runs, and nothing should: a lock would be machinery for a case a sentence covers.

## Why the phase timeout is tunable

`CARGOSHIP_E2E_TOFU_PHASE_TIMEOUT` exists for diagnosis, not for tuning. A phase that is going to fail waits out its whole timeout first, so investigating at the module's own 20 minutes costs twenty minutes to arrive at an error that six reaches in six. It is the `timeout` the configuration hands the provider, so it changes nothing else.

## What has not been proven

The suite is new and its upper half has never run green. Verified: `init` resolves the provider from the mirror with no registry, `plan` renders the resource with the fleet keyed by machine name and `removed = (known after apply)` per host, the package builds from the shipped k3s definition, and the controller installs and reports `Ready` with no restarts. **The apply, kubeconfig, convergence, removal and destroy steps have not been seen pass** - they were blocked by the inter-container networking failure above on the machine available at the time, which is a host fix rather than a code one.

Also uncovered, by design for now:

- **The data source.** `cargoship_cluster_facts` still takes `host` blocks rather than a map, and nothing here exercises it.
- **Terragrunt.** The suite runs `tofu` directly; the layout in `example/terragrunt` is checked by hand.
- **A controller removal.** The removal step removes a worker. Removing a controller gives up an etcd member, and the rules around it are unit-tested and documented rather than proven here - see [choice-tofu-host-removal](choice-tofu-host-removal.md).
- **An OCI mirror.** The provider resolves out of a `filesystem_mirror`; the `oci_mirror` path a release publishes to is checked by reading the artifact with the ORAS CLI.
