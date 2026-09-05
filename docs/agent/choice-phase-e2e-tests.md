# Why the cluster e2e suite steps through the apply phases one at a time

The cluster suite (`src/test/e2e/cluster`) used to be a lifecycle test: provision a bootloose cluster, run `cargoship apply` once, then check that the cluster came up. It proved that apply works end to end and nothing else. When a phase left the wrong artifact behind but the install still succeeded -- a `/etc/hosts` entry written with the wrong address that DNS happened to cover, a lock file that was never cleaned up, a firewall rule that named the wrong peer -- the suite stayed green. When it did fail, the failure was "the cluster is not healthy" and the phase responsible had to be found by reading the log.

The suite now drives the apply phase list in-process, one phase at a time, and asserts what each phase left on the hosts before the next one runs. This document records the choices that shape it, because several of them look arbitrary in the code and each had an alternative worth stating.

## Replacing the CLI apply rather than adding to it

The obvious way to add phase-level coverage is to leave the existing `cargoship apply` test alone and add a second suite beside it. That was rejected: a bootloose cluster is the expensive part of the run, and installing rke2 onto it twice costs several minutes and a lot of memory for coverage that overlaps almost entirely.

So the phase walk *is* the install. `Test_00_CreatePackage` builds the package and `Test_01_Prepare` readies the hosts, the phase tests then perform the install one phase at a time, and a whole `apply` survives only as `Test_ZZ2_ApplyIsIdempotent`, which runs after the cluster is already up. That step now earns its place twice over: it is the idempotency check *and* the only test that exercises `action.NewApply`'s own wiring -- the phase list, the flag plumbing, the ordering -- which the phase tests deliberately bypass.

None of these steps shell out. `Test_00_CreatePackage` calls `distro.Create`, and the four whole-action steps call `action.NewPrepare`, `action.NewApply`, `action.NewReset` and `action.NewKubeConfig`, each with its own manager built the way the matching command builds one and its own temp directory to remove. The suite was written against the CLI first, and the binary turned out to be doing nothing the packages could not: it cost a build step in CI, a rebuild-before-you-run rule, and a class of confusing failure where a stale binary disagreed with the source being read. What is lost is coverage of the cobra layer -- flag parsing, `--confirm`, the config file resolution -- which `src/test/e2e/noncluster` covers against the real binary and does so in seconds.

The cost is that the phase tests do not test `action.NewApply`. If a phase were dropped from the list in `src/pkg/action/apply.go`, the phase tests would keep passing, because they build their own list. `Test_ZZ2` is what catches that, and the ordering comment in the suite doc is what keeps the two lists in sync by hand. A generated list, read from `action` and iterated, was considered and rejected: it would make the assertions dynamic, and the whole point is that each phase has a hand-written assertion about the artifact it leaves.

## One number per phase, taken from `src/pkg/phase`

Testify runs suite methods in lexicographic order of the method name, which is the only ordering mechanism available. So the method name has to carry a number, and there were two candidates: the phase's position in the apply order, or the number of its source file.

The suite first used the apply order, on the grounds that the point of the suite is to reproduce what apply does. It made the test for `phase/25_modify_hosts_file.go` a method called `Test_12_ModifyHosts` living in `25_modify_hosts_file_test.go`, and every file in the package carried two numbers that disagreed. Inserting a phase anywhere but the end renumbered every method after it, which is a large diff that says nothing, and cross-references between test comments had to be renumbered with it.

The numbers are now the source file's, in both places: `25_modify_hosts_file_test.go` contains `Test_25_ModifyHosts`. One number per phase, so the test for a phase is findable from the phase and the ordering needs no separate bookkeeping. Adding a phase adds a file and a method and renumbers nothing.

`src/pkg/phase` numbers its files by rough category -- `20`-`26` prepare the host, `50`-`59` upload, `60`-`72` install and sync, `80`-`81` finish, `91`-`92` lock and unlock -- and that ordering happens to agree with apply's for every phase but one. Apply takes the lock third, right after OS detection, so that it is held for the whole run; the file number puts it after the install instead, with unlock immediately behind it.

That is the cost, and it is worth naming precisely. `Test_91_Lock` still asserts the thing the phase is responsible for: a lock file on every host holding this process's instance ID, removed again by `Test_92_Unlock`. What is no longer covered is the lock being *held* while the other phases run, so a phase that misbehaved against a locked cluster would not be caught here. Nothing in the current phase list reads the lock, and `Test_ZZ2_ApplyIsIdempotent` runs a real `cargoship apply`, which takes the lock in its proper place. Special-casing the lock -- running it early under a method name that does not match its file -- was considered and rejected: it reintroduces the two-number problem for one phase, which is the shape of thing that is forgotten and then misread.

Steps that are not phases have no source file to take a number from. They use the ends of the ordering instead: `Test_00_CreatePackage` and `Test_01_Prepare` sort before every phase number, `Test_ZZ1` and `Test_ZZ2` sort after every phase number because a letter sorts after a digit.

## One shared harness, and tests that cannot run alone

Phases communicate through the hosts. `DetectOS` resolves a `Configurer` and hangs it on the host; `GatherFacts` fills in `Metadata`; `Connect` leaves a live SSH connection that every later phase reuses; the upload phases record `Metadata.Install` for the initialize phases to call. Running a phase in isolation means reconstructing all of that, which means reimplementing its predecessors.

So there is one `phaseHarness` for the whole suite, built once in `Test_05_Manager`, and the phase tests are ordered and stateful by design. `go test -run 'TestClusterPhases/apply/Test_25_ModifyHosts'` does not work and is not meant to: the hosts were never connected. This is stated in the suite doc comment because it is the first thing someone debugging a failure will try.

The harness builds its manager from exactly the inputs `action.NewApply` uses -- `load.ClusterDefinition` for the inventory, `distro.Load` for the package, `registry.GetDistroModuleBuilder` for the distro module -- so the phases see the state they would see under a real apply. `phase.Manager.Run` is called once per phase with a single-element `Phases`, which is safe because `Run` calls `defaults.Set` each time and that is idempotent.

## Asserting the artifact, not the return code

A phase that returns `nil` has not necessarily done anything. Every assertion in the suite reads the host back over SSH and checks the thing an operator would check:

- the lock file's *content* on every host, matching `hostname-pid`, not merely its existence
- every peer's private address, long hostname and short hostname in every host's `/etc/hosts`, plus `getent hosts <peer>` resolving to the right address -- the full matrix, because a broken loop that writes only the first peer passes any spot check
- `sysctl -n <key>` matching the rendered value, so a setting written to `/etc/sysctl.d` but never applied fails
- the firewall's own dump (`firewall-cmd --list-all`, `ufw status verbose`, `nft list ruleset`) naming each peer, rather than the phase's idea of what it wrote
- the engine config on disk containing the host's name and data directory
- the service actually active and `RunningVersion` matching the package's version
- the kubeconfig parsed with `clientcmd`, checking the context name, the server URL and that the embedded CA and client material are non-empty

Reading through SSH rather than through the phase's own state is deliberate: a phase that records success in `Metadata` without touching the host would otherwise pass.

## Phases that legitimately do nothing assert the gate

Several phases are expected to skip on this cluster: SELinux and fapolicyd where neither is active, the upgrade phases on a fresh install, the config-sync phases when nothing has drifted. Asserting nothing there would be a test that cannot fail.

Instead those tests compute what the hosts report -- how many have SELinux enabled, how many run fapolicyd, whether the package carries a rule set -- and assert that `ran(p)` matches. That tests the routing logic, which is the part most likely to break, and it keeps working when the cluster changes: adding an image with SELinux enabled turns the same assertion into a check that the phase fires and installs `container-selinux`.

`ran(p)` type-asserts `interface{ ShouldRun() bool }` and is only meaningful after `harness.run`, because the manager calls `Prepare` first and most gates are computed there. The manager's own list of executed phases is unexported, which is why the gate is re-derived rather than read back.

## Stopping after the first failure

Thirty-one ordered, stateful tests mean one broken early phase produces thirty downstream failures that say nothing. `TearDownTest` records the failure and `SetupTest` skips the rest, so the output names one phase. Teardown of the containers is unaffected -- `TestMain` deletes the bootloose cluster regardless -- so this costs nothing but the coverage that was already lost.

## Nine nodes across two OS families

The cluster was six usable nodes on one Ubuntu image (with three further machines provisioned and never wired into the inventory). It is now nine: three controllers and six workers, each role split between Ubuntu and Fedora.

The OS mix is the substantive part. The apply phase list routes on OS family in at least five places -- the RPM and APT upload phases, SELinux, fapolicyd, and the firewall backend -- and on a single-family cluster the branch that does not match is never taken. Worse, the gate assertions described above *pass* in that case, so the suite reports coverage it does not have. With both families present, `Test_57_RPMUploadFiles` and `Test_58_APTUploadFiles` each assert a real split: the family they claim keeps what was staged, the family they do not comes back byte-identical.

To keep that from silently regressing, `Test_09_DetectOS` asserts that every family the bootloose config provisions is present, and that each host's detected `os-release` ID matches the image its machine was built from. If someone collapses the cluster back to one image, the failure names that cause rather than showing up as a phase test that quietly stopped covering a branch.

Nine nodes rather than six also puts more than one batch through the worker concurrency path: the initialize and upgrade phases batch workers at `WorkerConcurrent`, which the suite sets to `5`, and six workers is the first count that makes that a real batching decision.

Both images are the project's own: `ghcr.io/colonel-byte/bootloose/ubuntu-26` and `ghcr.io/colonel-byte/bootloose/fedora-44`. The Fedora replicas are named `kcf*` and `kwf*` so that the inventory's existing `kc`/`kw` prefix mapping claims them without the OS family entering the mapping at all.

## A tenth machine that receives uploads and never joins

Two of the three upload phases are reached by an OS-family filter. The third, `phase/59_bin_install.go`, is what a host gets when neither of the other two claims it -- which on this cluster meant every host, because rke2 ships as neither an RPM nor a deb. So the phase was being exercised as the path every host happens to take, not as the fallback it is written to be, and nothing in the suite would have noticed if the two family filters had started claiming hosts they should not.

The cluster therefore also provisions one `ghcr.io/colonel-byte/bootloose/alpine-3.23` machine. Alpine is neither Enterprise Linux nor Debian, so `utils.FilterEnterpriseLinux` and `utils.FilterDebianLinux` both decline it and it reaches the BIN phase as the only host that had nowhere else to go. `Test_59_BINUploadFiles` asserts against it by name.

It cannot run the engine: rke2 links against glibc and Alpine is musl. Three things follow from that, and each had an alternative.

**The host is a worker in the inventory, not a role of its own.** The upload phases select by role, so a host with no role would not have been claimed and the whole point would be lost. It is named `kwa0`, which the inventory's `kw` prefix maps to the worker role with no special case, and `uploadOnlyPrefix` in `cluster_inventory.go` is what marks it as more than a worker. The name template in `main_test.go` is built from that same constant so the two cannot drift.

**It is removed from the manager after the last upload phase, rather than skipped by each later test.** `Test_60_ConfigureEngine` calls `harness.dropUploadOnlyHosts()` before it runs anything. Skipping was tried first and is not sufficient: `InitializeWorkers.Prepare` claims every non-controller host whose agent is not already running, with no OS gate at all, so a test that merely declined to assert on the Alpine host would still have watched the phase spend its retry budget trying to install rke2 on it. Removing the host from `manager.Config.Spec.Hosts` is the only thing that stops the phase from selecting it. The harness keeps what it dropped so that `close` can disconnect it, since the Disconnect phase only sees the hosts still on the manager.

**There are two generated inventory files.** `generated-cluster-full.yaml` has all ten machines and is what the harness is built from; `generated-cluster.yaml` has the nine that join and is what `e2e.ClusterConfigPath` points at. The whole-action steps -- `Test_01_Prepare`, `Test_ZZ2_ApplyIsIdempotent`, and `ResetSuite`'s two -- load that path, because an action has no notion of an upload-only host and would try to install the engine on Alpine. A single file with an annotation the loader ignores was considered and rejected: it would put a test-only concept into the inventory schema.

The boundary is worth stating plainly, because it is the thing a future phase can break without any test failing: **phases 50 through 59 see ten hosts, everything after them sees nine.** A new upload phase belongs before the drop. Anything that installs, starts or queries the engine belongs after it.

## Three walks against one cluster, in one parent test

The suite now runs three suites rather than one: `ApplyPhaseSuite` installs the distro, `UpgradePhaseSuite` walks the same phases again with a newer package, and `ResetSuite` takes the distro back off. They share one bootloose cluster and one kubeconfig, and they only work in that order.

Go runs top-level test functions in the order they are declared across the package's files, sorted by file name -- which is an ordering nobody declared and that a renamed file silently changes. So there is one top-level test, `TestClusterPhases` in `main_test.go`, and the three walks are `t.Run` subtests of it. The apply subtest's result is checked: if the install failed there is no cluster for the other two to walk, and the run returns rather than reporting three failures for one cause. The upgrade subtest's result is not checked, because a half-upgraded cluster is still something reset has to be able to tear down.

That is also why `reset` is no longer `Test_ZZ3`/`Test_ZZ4` on the apply suite. Reset has to run last, and the upgrade walk has to run in between, so the two steps moved into `ResetSuite`. It loads no package -- reset and kube-config both build a bare manager -- so it needs neither a harness nor a package path, only the `distroID` constant.

`kubeconfigPath` moved to `TestMain` for the same reason. All three walks touch the same file: the apply walk writes it, the upgrade walk rewrites it against the upgraded control plane, and the reset walk asserts it survives a teardown that can no longer reach a controller. A suite-owned `t.TempDir()` would have been deleted between them.

## Walking the upgrade, and where it starts

On a fresh install `phase/66_upgrade_controller.go` and `phase/67_upgrade_worker.go` claim no hosts, and the config-sync phases find no drift. The apply walk asserts exactly that -- correctly, it is the routing that matters -- but it means the code inside those phases was never executed by any test. Draining a node, stopping the service, running the install hook, waiting for Ready and uncordoning is the most destructive sequence in the phase list and the only one with no coverage at all.

`UpgradePhaseSuite` closes that. It builds a second package from `example/rke2-cilium/v1_35/v1.35.0-rke2r3` and walks the phases against the cluster the apply walk left running.

**The target is the next patch of the same minor version.** `v1.35.0-rke2r1` and `v1.35.0-rke2r3` differ in the engine tarball, three RPM URLs and the container image tags, and in nothing else. The next minor, `v1.35.1-rke2r1`, also moves Cilium from 1.18.4 to 1.19.0, which adds a CNI upgrade and a second set of image pulls to a walk that is testing neither. The upgrade phases route on `VersionLess`, which compares the prerelease identifier, so `r1 < r3` is a real upgrade as far as every phase under test is concerned.

**It starts at `GatherFactsDistro`, not at `Connect`.** Connect, DetectOS, GatherFacts and ValidateHosts do the same work against the same hosts they did during the install, and the apply walk already asserts each of them one phase at a time. Repeating that costs runtime and adds no assertion that could fail differently, so they run as a single `Test_01_Reconnect` step whose only job is to hand the rest of the walk a connected inventory. From `Test_12_GatherFactsDistro` on, every phase gets its own method, because from there on every phase behaves differently than it did on the install.

**The lock phases are left out.** They are asserted in the apply walk, and taking the lock again for a second walk against the same hosts would be testing the lock file, not the upgrade.

**The assertions that differ are the point.** `Test_12` is where the divergence starts: the hosts now report the installed version, and the test asserts it is `VersionLess` than the packaged one *by calling the same comparison the phases' own `Prepare` calls*, so the test cannot disagree with the routing it is asserting. `Test_61` and `Test_62` must claim nothing, which is the inverse of the apply walk's assertion and the check that an initialize phase never re-bootstraps a live node. `Test_66` and `Test_67` are the only place the upgrade phases are asserted to have run, and they check all three outcomes of the sequence: the service came back up, `RunningVersion` is now the packaged version, and the node is not left cordoned. That last one is worth having on its own -- a node left `Unschedulable` looks healthy from the host side, service running and version correct, and only shows up later as a cluster that will not schedule.

Where the two walks assert the same thing -- the upload phases, the engine config, the kubeconfig, the labels, disconnect -- the body lives once, as an unexported method on the shared `phaseWalk` that both suites embed, and each suite's `Test_NN` method is a one-line call to it. Testify collects methods by their `Test` prefix, so an unexported method is shared rather than run twice. Duplicating those bodies was the alternative, and it would have meant ten pairs of assertions that have to be kept in step by hand.

## The upgrade is opt-in

The upgrade walk runs only when `CARGOSHIP_E2E_UPGRADE` is set, and in CI only on a `workflow_dispatch` input or the `e2e-cluster-upgrade` label.

The install walk already sits close to what a hosted `ubuntu-latest` runner can do: nine rke2 nodes on four cores and 16GB, with the workflow deleting several gigabytes of preinstalled toolchains to make the images fit in the ~14GB the runner leaves free. The upgrade imports a second complete set of engine images into all nine containerd stores, so it roughly doubles the disk and adds tens of minutes. Making it unconditional would mean every run of this workflow pays for it, including the runs that only want to know the install still works.

Running it by default and skipping in CI was the alternative. It was rejected because it inverts which environment is the odd one out: the failure mode is a suite that passes locally and cannot run in the place the label was added to make it run. A single environment variable, read in `SetupSuite`, is checked in one place and skips with a message naming the variable.

## Profiles on the generated inventory

The generated bootloose inventory set no `Profile`, which made `LabelNodes` a no-op: it iterates hosts, skips those with an empty profile, and would have labelled nothing. Each host now gets a profile equal to its role.

This was checked against every other consumer of `Profile` before making the change. `GatherFacts.setupProfileOverrides` looks the profile up in `Spec.Config.Profiles` and does nothing when it is absent. The per-profile concurrency grouping resolves through `ZarfClusterProfiles.ResolveConcurrency`, which falls back to the caller's value for an unmapped profile. The file selectors in `55_files_common.go` compare against the *role* constants passed by the RPM/APT/BIN phases, never against `h.Profile`. So the profile is inert everywhere except the phase under test, which is what makes it safe to set purely to make that phase observable.
