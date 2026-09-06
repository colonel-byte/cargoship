# Why the cluster e2e suite steps through the apply phases one at a time

The cluster suite (`src/test/e2e/cluster`) used to be a lifecycle test: provision a bootloose cluster, run `cargoship apply` once, then check that the cluster came up. It proved that apply works end to end and nothing else. When a phase left the wrong artifact behind but the install still succeeded -- a `/etc/hosts` entry written with the wrong address that DNS happened to cover, a lock file that was never cleaned up, a firewall rule that named the wrong peer -- the suite stayed green. When it did fail, the failure was "the cluster is not healthy" and the phase responsible had to be found by reading the log.

The suite now drives the apply phase list in-process, one phase at a time, and asserts what each phase left on the hosts before the next one runs. This document records the choices that shape it, because several of them look arbitrary in the code and each had an alternative worth stating.

## Replacing the CLI apply rather than adding to it

The obvious way to add phase-level coverage is to leave the existing `cargoship apply` test alone and add a second suite beside it. That was rejected: a bootloose cluster is the expensive part of the run, and installing rke2 onto it twice costs several minutes and a lot of memory for coverage that overlaps almost entirely.

So the phase walk *is* the install. `Test_00_CreatePackage` builds the package and `Test_01_Prepare` readies the hosts, and the phase tests then perform the install one phase at a time.

None of these steps shell out. `Test_00_CreatePackage` calls `distro.Create` and `Test_01_Prepare` calls `action.NewPrepare`, each with its own manager built the way the matching command builds one and its own temp directory to remove. The suite was written against the CLI first, and the binary turned out to be doing nothing the packages could not: it cost a build step in CI, a rebuild-before-you-run rule, and a class of confusing failure where a stale binary disagreed with the source being read. What is lost is coverage of the cobra layer -- flag parsing, `--confirm`, the config file resolution -- which `src/test/e2e/noncluster` covers against the real binary and does so in seconds.

The cost is that the phase tests do not test `action.NewApply`. If a phase were dropped from the list in `src/pkg/action/apply.go`, the phase tests would keep passing, because they build their own list. Keeping the two lists in sync is done by hand, from the ordering comment in the suite doc. A generated list, read from `action` and iterated, was considered and rejected: it would make the assertions dynamic, and the whole point is that each phase has a hand-written assertion about the artifact it leaves.

## One number per phase, taken from `src/pkg/phase`

Testify runs suite methods in lexicographic order of the method name, which is the only ordering mechanism available. So the method name has to carry a number, and there were two candidates: the phase's position in the apply order, or the number of its source file.

The suite first used the apply order, on the grounds that the point of the suite is to reproduce what apply does. It made the test for `phase/25_modify_hosts_file.go` a method called `Test_12_ModifyHosts` living in `25_modify_hosts_file_test.go`, and every file in the package carried two numbers that disagreed. Inserting a phase anywhere but the end renumbered every method after it, which is a large diff that says nothing, and cross-references between test comments had to be renumbered with it.

The numbers are now the source file's, in both places: `25_modify_hosts_file_test.go` contains `Test_25_ModifyHosts`. One number per phase, so the test for a phase is findable from the phase and the ordering needs no separate bookkeeping. Adding a phase adds a file and a method and renumbers nothing.

`src/pkg/phase` numbers its files by rough category -- `20`-`26` prepare the host, `50`-`59` upload, `60`-`72` install and sync, `80`-`81` finish, `91`-`92` lock and unlock -- and that ordering agrees with apply's for every phase but the lock, which apply takes third and the file number puts after the install.

Steps that are not phases have no source file to take a number from. They use the ends of the ordering instead: `Test_00_CreatePackage` and `Test_01_Prepare` sort before every phase number, and anything that has to run after every phase takes a `Test_ZZ` name, because a letter sorts after a digit.

## One shared harness, and tests that cannot run alone

Phases communicate through the hosts. `DetectOS` resolves a `Configurer` and hangs it on the host; `GatherFacts` fills in `Metadata`; `Connect` leaves a live SSH connection that every later phase reuses. Running a phase in isolation means reconstructing all of that, which means reimplementing its predecessors.

So there is one `phaseHarness` for the whole suite, built once in `Test_05_Manager`, and the phase tests are ordered and stateful by design. `go test -run 'TestClusterPhases/apply/Test_25_ModifyHosts'` does not work and is not meant to: the hosts were never connected. This is stated in the suite doc comment because it is the first thing someone debugging a failure will try.

The harness builds its manager from exactly the inputs `action.NewApply` uses -- `load.ClusterDefinition` for the inventory, `distro.Load` for the package, `registry.GetDistroModuleBuilder` for the distro module -- so the phases see the state they would see under a real apply. `phase.Manager.Run` is called once per phase with a single-element `Phases`, which is safe because `Run` calls `defaults.Set` each time and that is idempotent.

The assertion bodies live on `phaseWalk`, an unexported type `ApplyPhaseSuite` embeds, with a one-line `Test_NN` method on the suite calling each one. For a single walk that is more indirection than it needs; it is there because further walks over the same phase list -- an upgrade against the installed cluster, a join of a new node -- assert the same things and would otherwise have to copy the bodies and keep them in step by hand. Testify collects methods by their `Test` prefix, so an unexported method on the embedded type is shared rather than run as a test of its own.

## Asserting the artifact, not the return code

A phase that returns `nil` has not necessarily done anything. Every assertion in the suite reads the host back over SSH and checks the thing an operator would check:

- every host's detected `os-release` ID matching the image its machine was built from, and every family the bootloose config provisions being present
- the rendered `/etc/sysctl.d/99-cargoship.conf` naming every setting the package carries, at the value it carries
- every peer's private address, long hostname and short hostname in every host's `/etc/hosts`, plus `getent hosts <peer>` resolving to the right address -- the full matrix, because a broken loop that writes only the first peer passes any spot check
- the firewall's own dump (`firewall-cmd --list-all`, `ufw status verbose`, `nft list ruleset`) naming each peer, rather than the phase's idea of what it wrote
- every path the upload manifest claims present on the host, or staged beside it under the temporary name the install hook will move

Reading through SSH rather than through the phase's own state is deliberate: a phase that records success in `Metadata` without touching the host would otherwise pass.

The sysctl test is the one place where the artifact is the file and not the effect. The nodes are containers sharing the host kernel, and the settings the phase writes are not namespaced, so `sysctl -n` there reports whatever the machine running the tests is set to: asserting on it would fail a correct phase and pass a broken one whenever the host already matched. The phase still runs `sysctl --system` itself, and a failure there fails the phase.

## Phases that legitimately do nothing assert the gate

Several phases are expected to skip on this cluster: SELinux and fapolicyd where neither is active, and later the config-sync phases when nothing has drifted. Asserting nothing there would be a test that cannot fail.

Instead those tests compute what the hosts report -- how many have SELinux enabled, how many run fapolicyd, whether the package carries a rule set -- and assert that `ran(p)` matches. That tests the routing logic, which is the part most likely to break, and it keeps working when the cluster changes: adding an image with SELinux enabled turns the same assertion into a check that the phase fires and installs `container-selinux`.

`ran(p)` type-asserts `interface{ ShouldRun() bool }` and is only meaningful after `harness.run`, because the manager calls `Prepare` first and most gates are computed there. The manager's own list of executed phases is unexported, which is why the gate is re-derived rather than read back.

## Stopping after the first failure

Ordered, stateful tests mean one broken early phase produces a run of downstream failures that say nothing. `TearDownTest` records the failure and `SetupTest` skips the rest, so the output names one phase. Teardown of the containers is unaffected -- `TestMain` deletes the bootloose cluster regardless -- so this costs nothing but the coverage that was already lost.

## Nine nodes across two OS families

The cluster was six usable nodes on one Ubuntu image (with three further machines provisioned and never wired into the inventory). It is now nine: three controllers and six workers, each role split between Ubuntu and Fedora.

The OS mix is the substantive part. The apply phase list routes on OS family in at least five places -- the RPM and APT upload phases, SELinux, fapolicyd, and the firewall backend -- and on a single-family cluster the branch that does not match is never taken. Worse, the gate assertions described above *pass* in that case, so the suite reports coverage it does not have. With both families present, `Test_57_RPMUploadFiles` and `Test_58_APTUploadFiles` each assert a real split: the family they claim keeps what was staged, the family they do not comes back byte-identical.

To keep that from silently regressing, `Test_09_DetectOS` asserts that every family the bootloose config provisions is present, and that each host's detected `os-release` ID matches the image its machine was built from. If someone collapses the cluster back to one image, the failure names that cause rather than showing up as a phase test that quietly stopped covering a branch.

Nine nodes rather than six also puts more than one batch through the worker concurrency path: the initialize phases batch workers at `WorkerConcurrent`, which the suite sets to `5`, and six workers is the first count that makes that a real batching decision.

Both images are the project's own: `ghcr.io/colonel-byte/bootloose/ubuntu-26` and `ghcr.io/colonel-byte/bootloose/fedora-44`. The Fedora replicas are named `kcf*` and `kwf*` so that the inventory's existing `kc`/`kw` prefix mapping claims them without the OS family entering the mapping at all.

## A tenth machine that receives uploads and never joins

Two of the three upload phases are reached by an OS-family filter. The third, `phase/59_bin_install.go`, is what a host gets when neither of the other two claims it -- which on an all-Ubuntu, all-Fedora cluster meant every host, because rke2 ships as neither an RPM nor a deb. So the phase was being exercised as the path every host happens to take, not as the fallback it is written to be, and nothing in the suite would have noticed if the two family filters had started claiming hosts they should not.

The cluster therefore also provisions one `ghcr.io/colonel-byte/bootloose/alpine-3.23` machine. Alpine is neither Enterprise Linux nor Debian, so `utils.FilterEnterpriseLinux` and `utils.FilterDebianLinux` both decline it and it reaches the BIN phase as the only host that had nowhere else to go. `Test_59_BINUploadFiles` asserts against it by name.

It cannot run the engine: rke2 links against glibc and Alpine is musl. Three things follow from that, and each had an alternative.

**The host is a worker in the inventory, not a role of its own.** The upload phases select by role, so a host with no role would not have been claimed and the whole point would be lost. It is named `kwa0`, which the inventory's `kw` prefix maps to the worker role with no special case, and `uploadOnlyPrefix` in `cluster_inventory.go` is what marks it as more than a worker. The name template in `main_test.go` is built from that same constant so the two cannot drift.

**It has to be removed from the manager before the engine phases, rather than skipped by each later test.** Skipping was tried first and is not sufficient: `InitializeWorkers.Prepare` claims every non-controller host whose agent is not already running, with no OS gate at all, so a test that merely declined to assert on the Alpine host would still have watched the phase spend its retry budget trying to install rke2 on it. Taking it off `manager.Config.Spec.Hosts` is the only thing that stops the phase from selecting it, and the harness keeps what it dropped so that `close` can disconnect it, since the Disconnect phase only sees the hosts still on the manager.

**There are two generated inventory files.** `generated-cluster-full.yaml` has all ten machines and is what the harness is built from; `generated-cluster.yaml` has the nine that join and is what `e2e.ClusterConfigPath` points at. The whole-action steps load that path, because an action has no notion of an upload-only host and would try to install the engine on Alpine. A single file with an annotation the loader ignores was considered and rejected: it would put a test-only concept into the inventory schema.

## Profiles on the generated inventory

The generated bootloose inventory set no `Profile`, which makes `LabelNodes` a no-op: it iterates hosts, skips those with an empty profile, and would have labelled nothing. Each host now gets a profile equal to its role.

This was checked against every other consumer of `Profile` before making the change. `GatherFacts.setupProfileOverrides` looks the profile up in `Spec.Config.Profiles` and does nothing when it is absent. The per-profile concurrency grouping resolves through `ZarfClusterProfiles.ResolveConcurrency`, which falls back to the caller's value for an unmapped profile. The file selectors in `55_files_common.go` compare against the *role* constants passed by the RPM/APT/BIN phases, never against `h.Profile`. So the profile is inert everywhere except the phase it makes observable, which is what makes it safe to set.
