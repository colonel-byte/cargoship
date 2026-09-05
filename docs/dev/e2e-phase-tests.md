# Adding an E2E Test for a New Apply Phase

The cluster suite in `src/test/e2e/cluster` walks the apply phase list one phase at a time against a live bootloose cluster and asserts what each phase left on the hosts. Adding a phase to `src/pkg/action/apply.go` without adding a test here leaves a gap that nothing else covers, so this page is the checklist for closing it.

One top-level test, `TestClusterPhases`, runs three walks against the same cluster in the order they work in: `apply` installs the distro, `upgrade` walks the same phases again with a newer package, and `reset` takes the distro back off. A new phase needs a test in the apply walk, and usually one in the upgrade walk too -- see [Adding the upgrade half](#adding-the-upgrade-half).

For why the suite is built this way, see [choice-phase-e2e-tests](../agent/choice-phase-e2e-tests.md). For running the e2e suites generally, see [e2e-tests](e2e-tests.md).

## One number per phase

A phase is identified by its source file's number, and that number is used twice:

*   **The test file name mirrors the phase's source file.** A test for `src/pkg/phase/25_modify_hosts_file.go` goes in `src/test/e2e/cluster/25_modify_hosts_file_test.go`. Same number, same name, one file each.
*   **The method name carries the same number.** That file contains `Test_25_ModifyHosts`.

Testify runs suite methods in lexicographic order of the method name, so the number is also the order the phases run in here. The numbers are zero-padded to two digits, which makes lexicographic order numeric order.

That order matches apply's everywhere except the lock. Apply takes the lock third, right after OS detection, and holds it for the whole run; `phase/91_lock.go`'s number puts it after the install instead, with `phase/92_unlock.go` right behind it. The lock test still asserts what the phase writes on each host. What it no longer does is hold the lock across the phases in between, so a phase that misbehaves while the cluster is locked is not something this suite would see.

Steps that are not phases -- creating the package, `prepare`, the health check -- have no phase number to take, so they use the two ends of the ordering: `Test_00` and `Test_01` sort before every phase, and `Test_ZZ1` onwards sort after every phase, because a letter sorts after a digit. They live in `cluster_lifecycle_test.go`, alongside `ResetSuite`, which is the whole of the `reset` walk. Those steps run whole actions rather than single phases -- `distro.Create`, `action.NewPrepare`, `action.NewApply`, `action.NewReset`, `action.NewKubeConfig` -- each with its own manager, and are given `e2e.ClusterConfigPath`, the nine-host inventory, rather than the ten-host one the harness is built from; see the section on the upload-only host below.

## Adding a phase

Take the new phase's file number for both the test file and the method. Nothing else is renumbered: inserting `phase/63_something.go` gives you `63_something_test.go` containing `Test_63_Something`, and it lands between `Test_62_InitializeWorkers` and `Test_66_UpgradeController` on its own.

If a phase is renumbered in `src/pkg/phase`, rename its test file and its method to match, and check that its new number still puts it after everything it depends on -- the number is the ordering, so a phase that moves in the source moves here too.

## Writing the test

Start from the phase's source file and ask what an operator would look at to confirm it did its job. That is the assertion. A phase returning `nil` is not evidence of anything -- read the host back.

A minimal test:

```go
// Test_NN_YourPhase covers phase/NN_your_phase.go. <One or two sentences on what the phase
// does and, therefore, what artifact this asserts.>
func (s *ApplyPhaseSuite) Test_NN_YourPhase() {
	s.runPhase(&phase.YourPhase{Distro: s.harness.distro})

	for _, host := range s.harness.hosts() {
		s.Require().Truef(host.FileExist(someArtifactPath),
			"%s: nothing at %s", host, someArtifactPath)
	}
}
```

`s.runPhase` runs the phase through the shared manager and fails the test if it errors. Do not call `s.harness.run` directly unless you need the error itself.

Use `s.Require()` rather than `s.Assert()`. These tests are ordered and stateful, so continuing after a failed assertion produces noise about state the failed assertion already told you was wrong.

## What the harness gives you

`phase_harness.go` holds everything shared. The pieces a new test is likely to want:

| | |
|---|---|
| `s.harness.hosts()` | every host currently on the manager |
| `s.harness.controllers()` / `s.harness.workers()` | the role split |
| `s.harness.engineWorkers()` | the workers that join the cluster, so `workers()` minus the upload-only hosts |
| `s.harness.uploadOnly()` | the hosts that receive uploads and never join |
| `s.harness.dropUploadOnlyHosts()` | removes those hosts from the manager; already called once, in `Test_60` |
| `s.harness.manager` | the live `phase.Manager`, for `Distro`, `DistroID`, `TempDirectory` |
| `s.harness.distro` | the distro module, for phases that take a `Distro` field |
| `s.harness.opts` | the apply options the suite runs with (`ModifyHosts`, `ModifyFirewall`, `WorkerConcurrent`, ...) |
| `s.harness.carriesFilesFor(selector)` | whether the package ships OS files for `config.SelectorRPM` / `SelectorAPT` / `SelectorBIN` |
| `readOnHosts(hosts, path)` | reads one path on every host, keyed by host string |
| `lockFileContent()` | the `hostname-pid` string the lock phase writes |
| `ran(p)` | whether the manager executed the phase or skipped it |

For anything else, the hosts carry a live `rig` connection: `host.ExecOutput`, `host.ReadFile`, `host.FileExist`, and `host.Configurer` are all available and are how most assertions are written.

## Phases that are expected to skip

Some phases legitimately do nothing on this cluster -- the upgrade phases on a fresh install, config sync when nothing has drifted, fapolicyd when no host runs it. Do not write a test that asserts nothing; assert the *gate* instead. Compute what the hosts report, then require that `ran(p)` matches it:

```go
var fapolicyd int
for _, host := range s.harness.hosts() {
	if host.Configurer.ServiceIsRunning(host, phase.FAPOLICYD) {
		fapolicyd++
	}
}
carriesRules := s.harness.manager.Distro.Spec.Config.OS.FAPolicyd != ""

p := &phase.PrepareFapolicy{}
s.runPhase(p)
s.Require().Equalf(fapolicyd > 0 && carriesRules, ran(p),
	"phase ran=%v with %d fapolicyd hosts and rules=%v", ran(p), fapolicyd, carriesRules)

if !ran(p) {
	return
}
// ... assert the artifact where it did fire
```

This tests the routing, which is the part most likely to break, and it keeps working when the cluster gains a host that does trip the gate.

`ran(p)` is only meaningful *after* `s.runPhase`, because the manager calls `Prepare` first and most gates are computed there.

## Phases that route on the OS family

The cluster runs three OS families on purpose. Nine of the machines join the cluster -- three controllers and six workers, each role split between Ubuntu (`kc0`, `kc1`, `kw0`-`kw2`) and Fedora (`kcf0`, `kwf0`-`kwf2`) -- and a tenth, `kwa0`, runs Alpine. A phase that treats Enterprise Linux differently from Debian must be tested on both sides, not just on the side it claims.

Derive the host set from the same filters the phase uses, so the test cannot disagree with it:

```go
enterprise := s.harness.hosts().Filter(utils.FilterEnterpriseLinux)
debian := s.harness.hosts().Filter(utils.FilterDebianLinux)
```

Then assert the split: the hosts the phase claims got what it promises, the hosts it does not came back unchanged. `57_rpm_install_test.go` and `58_apt_install_test.go` are the worked examples.

Require the set you are testing to be non-empty. If someone collapses the cluster to one image, that turns a test which silently stopped covering anything into a failure that says so.

## The upload-only host, and the boundary at `Test_60`

Alpine is neither Enterprise Linux nor Debian, so both of the filters above decline it and it falls through to the BIN upload phase. That is why it is in the inventory: without it, `59_bin_install.go` is only ever exercised as the path every host happens to take, never as the fallback it is written to be.

It cannot run rke2, which links against glibc. So `Test_60_ConfigureEngine` calls `s.harness.dropUploadOnlyHosts()` before it does anything else, and from that point on the host is gone from the manager entirely. **Phases up to and including `Test_59` see ten hosts; everything after sees nine.**

This matters when adding a phase:

*   **An upload phase goes before the drop**, and should assert against `s.harness.uploadOnly()` explicitly if it is meant to claim those hosts.
*   **Anything that installs, starts, configures or queries the engine goes after the drop**, and needs nothing special -- `hosts()` and `workers()` already exclude the dropped hosts by then.
*   **Do not "fix" a phase that fails on Alpine by skipping the host in the assertion.** The phases select their own hosts in `Prepare`, with no OS gate in most cases, so the phase will still have run against it and spent its retry budget there. If a phase genuinely cannot run on a host, that host must not be on the manager when the phase runs.

`s.harness.engineWorkers()` exists for the one case where order is not obvious: a test that runs before the drop but is only meaningful for hosts that join.

## Adding the upgrade half

`UpgradePhaseSuite` walks the same phase list a second time, against the cluster the apply walk installed, with a package one patch release newer. It is what covers the code inside `phase/66_upgrade_controller.go` and `phase/67_upgrade_worker.go`: on a fresh install those phases claim no hosts, so the apply walk can only ever assert that they did nothing.

It starts at `Test_12_GatherFactsDistro`. Connect, DetectOS, GatherFacts and ValidateHosts run as a single `Test_01_Reconnect` step, because the apply walk already asserts them one at a time and they behave identically the second time round. The lock phases are left out for the same reason.

So a new phase needs an upgrade test if it is numbered `12` or higher. Which kind depends on what it does the second time:

*   **The phase does the same thing either way** -- an upload phase, a config render, anything that does not read the installed version. Move the body to an unexported method on `phaseWalk` and give each suite a one-line `Test_NN` method calling it. `50_uploadfiles_test.go` is the worked example. Testify collects methods by their `Test` prefix, so the shared method is called, not run as a test of its own. Start the shared body with `s.T().Helper()`.

*   **The phase behaves differently** -- anything gated on the version the hosts report. Write two methods with the same name, one per suite, in the same file. `61_initialize_controller_test.go` is the worked example: the apply walk requires `ran(p)`, the upgrade walk requires `!ran(p)`, and each says in its comment why.

```go
// Test_NN_YourPhase, on the upgrade walk, <what is different and why>.
func (s *UpgradePhaseSuite) Test_NN_YourPhase() {
	p := &phase.YourPhase{Distro: s.harness.distro}
	s.runPhase(p)
	s.Require().False(ran(p), "<the routing this proves>")
}
```

Two things the upgrade tests use that the apply ones do not:

| | |
|---|---|
| `installedVersion` | the version the apply walk put on the cluster, so what a host should report *before* the upgrade phase runs |
| `s.harness.manager.Distro.Spec.Version` | the version the upgrade package carries, so what it should report after |
| `s.harness.engineHosts()` | every host that runs the engine, so `hosts()` minus the upload-only ones -- the upload-only host still reports `phase.UnknownVersion` |
| `s.requireSchedulable(hosts)` | asserts each host's node is registered and not left cordoned, which is what the drain/uncordon half of an upgrade has to leave behind |

Assert version routing by calling the phases' own comparison rather than comparing strings:

```go
compare := &phase.GenericPhase{}
s.Require().True(compare.VersionLess(host, s.harness.manager.Distro.Spec.Version))
```

`VersionLess` reads no manager state, so a zero-valued phase is enough, and using it means the test cannot disagree with the routing it is asserting.

### Running it

The upgrade walk is off unless `CARGOSHIP_E2E_UPGRADE` is set. It installs a second complete set of engine images onto all nine nodes, which roughly doubles the disk the run needs and adds tens of minutes.

```console
$ CARGOSHIP_E2E_UPGRADE=1 go test -mod=vendor -count=1 -v -timeout=165m ./src/test/e2e/cluster/...
```

Without it, `TestClusterPhases/upgrade` skips with a message naming the variable, and the reset walk runs against the installed cluster as before.

## Prerequisites and running it

The suite needs Docker and a real distro package, and it is not fast: it provisions ten containers and installs rke2 onto nine of them. It needs no prebuilt binary -- every step calls the cargoship packages directly, so a bare checkout is enough.

```console
$ go test -mod=vendor -count=1 -v -timeout=60m ./src/test/e2e/cluster/...
```

Or through mage, which also clears leftover containers from a run that was killed before teardown. Unlike the other e2e mage targets, it builds nothing first:

```console
$ mage test:endToEndCluster           # install and reset
$ mage test:endToEndClusterUpgrade    # the same, with the upgrade walk in between
```

`-short` skips the whole suite, and `CARGOSHIP_E2E_UPGRADE` adds the upgrade walk. `TestMain` deletes the bootloose cluster on the way out even when tests fail.

### In CI

`.github/workflows/e2e-cluster.yaml` runs it, on its own trigger and separate from `e2e.yaml`, which is what runs on every pull request. This one does not: it provisions ten containers and installs rke2 onto nine of them, which is most of a hosted runner. It runs when triggered by hand from the Actions tab, and automatically on a pull request labelled `e2e-cluster`. Add that label to a PR touching `src/pkg/phase`, `src/pkg/action` or the inventory handling; the trigger listens for `labeled`, so labelling an open PR starts a run without needing a push.

The upgrade walk is a second opt-in on top of that: the `upgrade` input when dispatching by hand, or the `e2e-cluster-upgrade` label on a pull request. Either one implies the install walk, sets `CARGOSHIP_E2E_UPGRADE` for the job and raises its budget from 90 to 180 minutes. Use it on a PR that touches the upgrade phases, the version comparison, or anything the install walk can only assert has stayed out of the way.

The workflow has no build step and takes no artifact from `e2e.yaml`. Nothing in the suite runs a binary: `Test_00_CreatePackage` calls `distro.Create`, and the prepare, apply, reset and kube-config steps call the matching `action.New*` entry points. That is what makes the two workflows independent, which is the point of the split.

The job frees disk before it starts, because nine containerd image stores do not fit in what a hosted runner leaves free. If it fails with nodes that never reach Ready, check the diagnostics step for a full disk or an OOM kill before reading the phase failure as a real one -- that is the failure mode a nine-node cluster on four cores produces, and a larger runner is the fix.

## Things that will cost you time

*   **You cannot run one phase test on its own.** `-run 'TestClusterPhases/apply/Test_25_ModifyHosts'` fails: the hosts were never connected, because `Test_07_Connect` and `Test_09_DetectOS` are earlier methods that `-run` filtered out. Run the whole suite, or accept that reproducing one phase means reproducing its predecessors.
*   **The first failure stops the rest.** `SetupTest` skips every remaining step once one has failed, so the output names one phase rather than twenty-five. If the first failure looks like a symptom, it is the cause -- everything after it was skipped, not passed.
*   **Constants the phase package keeps unexported.** Where a path is not exported, the test file redeclares it with a comment saying where it came from (`uploadManifestPath` in `50_uploadfiles_test.go`, `sysctlConfPath` in `20_prepare_host_test.go`). Prefer exporting from `phase` when the constant is genuinely part of the contract; duplicate it, with the comment, when it is not.
*   **Ten containers is a lot of memory.** Nine of them each run an rke2 node, which is the reason this suite is not part of the default `go test ./...` path.
