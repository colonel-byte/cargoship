# Adding an E2E Test for a New Apply Phase

The cluster suite in `src/test/e2e/cluster` walks the apply phase list one phase at a time against a live bootloose cluster and asserts what each phase left on the hosts. Adding a phase to `src/pkg/action/apply.go` without adding a test here leaves a gap that nothing else covers, so this page is the checklist for closing it.

For why the suite is built this way, see [choice-phase-e2e-tests](../agent/choice-phase-e2e-tests.md). For running the e2e suites generally, see [e2e-tests](e2e-tests.md).

## One number per phase

A phase is identified by its source file's number, and that number is used twice:

*   **The test file name mirrors the phase's source file.** A test for `src/pkg/phase/25_modify_hosts_file.go` goes in `src/test/e2e/cluster/25_modify_hosts_file_test.go`. Same number, same name, one file each.
*   **The method name carries the same number.** That file contains `Test_25_ModifyHosts`.

Testify runs suite methods in lexicographic order of the method name, so the number is also the order the phases run in here. The numbers are zero-padded to two digits, which makes lexicographic order numeric order.

That order matches apply's everywhere except the lock. Apply takes the lock third, right after OS detection, and holds it for the whole run; `phase/91_lock.go`'s number puts it after the install instead, with `phase/92_unlock.go` right behind it. The lock test still asserts what the phase writes on each host. What it no longer does is hold the lock across the phases in between, so a phase that misbehaves while the cluster is locked is not something this suite would see.

Steps that are not phases -- creating the package, `prepare`, the health check, `reset` -- have no phase number to take, so they use the two ends of the ordering: `Test_00` and `Test_01` sort before every phase, and `Test_ZZ1` through `Test_ZZ4` sort after every phase, because a letter sorts after a digit. They live in `cluster_lifecycle_test.go`. Those steps shell out to the binary and are given `e2e.ClusterConfigPath`, the nine-host inventory, rather than the ten-host one the harness is built from; see the section on the upload-only host below.

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

## Prerequisites and running it

The suite needs Docker, the built binary, and a real distro package, and it is not fast: it provisions ten containers and installs rke2 onto nine of them.

```console
$ go build -mod=vendor -o "build/cargoship_$(go env GOOS)_$(go env GOARCH)" main.go
$ go test -mod=vendor -count=1 -v -timeout=60m ./src/test/e2e/cluster/...
```

Or through mage, which also clears leftover containers from a run that was killed before teardown:

```console
$ mage test:endToEndCluster
```

`-short` skips the whole suite. `TestMain` deletes the bootloose cluster on the way out even when tests fail.

## Things that will cost you time

*   **You cannot run one phase test on its own.** `-run 'TestApplyPhases/Test_25_ModifyHosts'` fails: the hosts were never connected, because `Test_07_Connect` and `Test_09_DetectOS` are earlier methods that `-run` filtered out. Run the whole suite, or accept that reproducing one phase means reproducing its predecessors.
*   **The first failure stops the rest.** `SetupTest` skips every remaining step once one has failed, so the output names one phase rather than twenty-five. If the first failure looks like a symptom, it is the cause -- everything after it was skipped, not passed.
*   **A stale binary.** `Test_00_CreatePackage` shells out to `build/cargoship_<goos>_<goarch>`. Rebuild after touching `src/`, and use `-count=1`.
*   **Constants the phase package keeps unexported.** Where a path is not exported, the test file redeclares it with a comment saying where it came from (`uploadManifestPath` in `50_uploadfiles_test.go`, `sysctlConfPath` in `20_prepare_host_test.go`). Prefer exporting from `phase` when the constant is genuinely part of the contract; duplicate it, with the comment, when it is not.
*   **Ten containers is a lot of memory.** Nine of them each run an rke2 node, which is the reason this suite is not part of the default `go test ./...` path.
