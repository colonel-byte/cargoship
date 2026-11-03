# The end-to-end suites, and what each backend actually covers

`test/e2e/noncluster` needs nothing but a built binary. `test/e2e/cluster` needs hosts, and where those hosts come from is chosen by `CARGOSHIP_E2E_BACKEND`. That choice decides which assertions can run at all, so it decides what a green run means. [`e2e-tests.md`](../docs/dev/e2e-tests.md) is the operator-facing reference; this file is about reading the results.

Fixtures are covered separately, in [`e2e/AGENTS.md`](e2e/AGENTS.md).

## The three backends

| Backend               | What provides the hosts                                     | Target                                      |
| --------------------- | ----------------------------------------------------------- | ------------------------------------------- |
| `bootloose` (default) | privileged containers, several OS families                  | `mage test:endToEndCluster`                 |
| `microvm`             | Rocky 10 qemu VMs on this machine, one distribution         | `mage test:endToEndClusterVM`               |
| `inventory`           | hosts that already exist, named by a `ZarfCluster` document | `mage test:endToEndClusterInventory <path>` |

`provision` in `backend_support_test.go` is the only place they differ. Everything downstream of the inventory it returns is identical, which is the point: a phase assertion cannot tell which backend it is running against, and should not.

## What each one cannot cover

**Containers report no SELinux, run no fapolicyd, and have no firewall of their own.** `21_prepare_selinux`, `22_prepare_fapolicyd` and the firewalld backend therefore do nothing there, correctly, and the one branch able to stop an apply is the branch never run. That is the whole reason the VM backend exists - see [`choice-microvm-backend.md`](../docs/agent/choice-microvm-backend.md).

**The VM backend cannot reach the cluster API from the host.** The fleet's nodes sit on a private segment and only the leader's forwarded ports reach the machine, so `requireClusterAPI` skips every step that talks to Kubernetes: `80_KubeConfig`, `81_LabelNodes`, `ZZ1_ClusterHealthy`. A VM run proves the phases did their work over SSH; it proves nothing about what the API server then thought. Those paths need a reachable cluster - a `k3d` cluster is enough to exercise a client change on its own.

**The VM backend runs one distribution.** Every family-routed branch that a mixed container fleet exercises - which phase claims which host, the `dnf` and `apt` splits - collapses to one answer. The two backends are additive, not alternatives.

**The external backend provisions nothing**, so the join walk is skipped. It also changes somebody's real hosts and does not change them back.

## Which distro the walks install

`CARGOSHIP_E2E_DISTRO` picks it: `k3s` (the suite default), `rke2` (what `mage test:endToEndClusterVM` sets) or `upstream`. It is read by two maps that have to stay in step - `exampleDefinitions` in `cluster_lifecycle_test.go` for the package the walks install, and `exampleUpgradeDefinitions` in `upgrade_suite_test.go` for the one the upgrade walk moves to. Both are plain maps keyed by the same string, so a distro missing from either resolves to an empty path and the walk fails trying to package `""` rather than saying what is unset.

**`upstream` does not complete yet.** It passes `dryrun` and gets through the upload phases, then fails at `Test_60_ConfigureEngine` on [#849](https://github.com/colonel-byte/cargoship/issues/849) - `ConfigPath()` is a directory for that distro and the assertion `cat`s it. That is a pre-existing defect in the distro interface, not a regression, and it is why `mage test:endToEndClusterVM` defaults to `rke2`.

**The upgrade walk is opt-in**, behind `CARGOSHIP_E2E_UPGRADE=1`. It gets through `Test_62_InitializeWorkers` and phase 66 completes, so a failure before that is worth reading as a defect rather than a known gap: [#850](https://github.com/colonel-byte/cargoship/issues/850) (the manifest keeping the previous engine's RPMs after an install consumes them, which failed `Test_59_BINUploadFiles` on every backend) and [#865](https://github.com/colonel-byte/cargoship/issues/865) (phase 66 draining a node by a name the cluster does not have) were both found here and are fixed. `Test_67_UpgradeWorkers`, `Test_71_EngineConfigSyncController` and `Test_72_EngineConfigSyncWorker` have not been exercised on any backend yet, so they are unproven rather than passing.

The walk also builds its own package, and nothing caches the `etcd` archive a `files` entry extracts `etcdctl` from - so every walk downloads it again, and a momentary DNS failure fails the walk at `Test_00_UpgradePackage` or `Test_00_JoinMachine` with nothing wrong in the phases at all. Three runs out of five in one session went that way. See [`todo-extractpath-archive-cache.md`](../docs/agent/todo-extractpath-archive-cache.md) before reading one of those as a regression.

## Reading a result

Count the skips, not just the failures. A skip here almost always means a capability the backend does not have, and a run with fifteen of them is not a pass of fifteen things. When reporting a run, say which walks passed, which steps skipped and why, and which failures are the backend rather than the code. `--- SKIP` lines carry their reason; the suite writes them for exactly this.

Three failures that are the environment rather than a defect, and recur:

- `a 4 node fleet at 4096MiB each needs 16384MiB, and only N is available` - another fleet is running. `mage dev:vmDown` for the `dev` fleet, `-fleet=e2e` for a run killed before its teardown.
- `fleet e2e is already up` - a previous run did not tear down. Same fix.
- `this machine cannot reach the cluster API` - the VM backend, by design, as above.

## The join walk, and why the backends do it differently

The join walk needs a host that was not there when the install ran. bootloose gives that for free: `CreateMachine` skips a container that already exists, so provisioning the fleet again creates only the new one and leaves the installed machines alone.

`microvm.Up` deliberately refuses a fleet that is already up rather than adopting it, because a node carrying the state of a previous run is the one thing that makes a phase failure impossible to reason about. A second provision is therefore a no-op plus one container on bootloose and a refusal on the VMs.

So the VM backend brings the fleet up **once, at the size the join walk will need**, and every later call selects out of it with `hostsNamedBy` instead of provisioning again. The apply walk is handed `clusterConfig()` and sees three hosts; the join walk is handed `clusterConfig().withJoinMachine()` and sees four. Holding a host out of a walk by narrowing the inventory is how the external backend already worked, so this reuses it rather than adding a second mechanism.

Two consequences to keep in mind:

- **The VM floor is the join walk's size, not the apply walk's.** Four nodes at the 4096MiB default is 16GiB at bring-up, before anything installs. That is the number to check against `free -m` when a run fails provisioning, and the reason to tear other fleets down first rather than after.
- **The join host exists for the whole run on the VMs.** It is provisioned and cloud-init'd from the start, held out of the inventory rather than out of existence, so nothing installs on it and it never joins the cluster. The join walk on this backend therefore tests "a host the inventory did not name" where bootloose tests "a host that did not exist". That is weaker, and worth remembering before concluding a join bug is fixed because the VM walk passed. Nothing in the phases discovers hosts by scanning rather than from the inventory, which is what makes the substitution sound; a phase that did would see straight through it.
