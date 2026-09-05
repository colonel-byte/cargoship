# Adding an E2E Test for a New Apply Phase

The cluster suite in `src/test/e2e/cluster` walks the apply phase list one phase at a time against a live bootloose cluster and asserts what each phase left on the hosts. Adding a phase to `src/pkg/action/apply.go` without adding a test here leaves a gap that nothing else covers, so this page is the checklist for closing it.

One top-level test, `TestClusterPhases`, runs the `apply` walk against the cluster it provisions. It is a subtest rather than a top-level test because later walks against the same cluster -- an upgrade, a reset -- hang off the same parent and have to run in a fixed order after it.

For why the suite is built this way, see [choice-phase-e2e-tests](../agent/choice-phase-e2e-tests.md). For running the e2e suites generally, see [e2e-tests](e2e-tests.md).

## One number per phase

A phase is identified by its source file's number, and that number is used twice:

*   **The test file name mirrors the phase's source file.** A test for `src/pkg/phase/25_modify_hosts_file.go` goes in `src/test/e2e/cluster/25_modify_hosts_file_test.go`. Same number, same name, one file each.
*   **The method name carries the same number.** That file contains `Test_25_ModifyHosts`.

Testify runs suite methods in lexicographic order of the method name, so the number is also the order the phases run in here. The numbers are zero-padded to two digits, which makes lexicographic order numeric order.

Steps that are not phases -- creating the package, `prepare` -- have no phase number to take, so they use the low end of the ordering: `Test_00` and `Test_01` sort before every phase. They live in `cluster_lifecycle_test.go`. Those steps run whole actions rather than single phases -- `distro.Create`, `action.NewPrepare` -- each with its own manager, and are given `e2e.ClusterConfigPath`, the nine-host inventory, rather than the ten-host one the harness is built from; see the section on the upload-only host below.

## Adding a phase

Take the new phase's file number for both the test file and the method. Nothing else is renumbered: inserting `phase/23_something.go` gives you `23_something_test.go` containing `Test_23_Something`, and it lands between `Test_22_PrepareFapolicy` and `Test_25_ModifyHosts` on its own.

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

Each assertion body lives on `phaseWalk`, the embedded type `ApplyPhaseSuite` is built from, with a one-line `Test_NN` method on the suite calling it. That is more indirection than one walk needs on its own, and it is what lets a second walk over the same phases reuse the body rather than copy it. Testify collects methods by their `Test` prefix, so the shared method is called, not run as a test of its own. Start a shared body with `s.T().Helper()`.

## What the harness gives you

`phase_harness.go` holds everything shared. The pieces a new test is likely to want:

| | |
|---|---|
| `s.harness.hosts()` | every host currently on the manager |
| `s.harness.controllers()` / `s.harness.workers()` | the role split |
| `s.harness.engineWorkers()` | the workers that join the cluster, so `workers()` minus the upload-only hosts |
| `s.harness.uploadOnly()` | the hosts that receive uploads and never join |
| `s.harness.manager` | the live `phase.Manager`, for `Distro`, `DistroID`, `TempDirectory` |
| `s.harness.distro` | the distro module, for phases that take a `Distro` field |
| `s.harness.opts` | the apply options the suite runs with (`ModifyHosts`, `ModifyFirewall`, `WorkerConcurrent`, ...) |
| `readOnHosts(hosts, path)` | reads one path on every host, keyed by host string |
| `ran(p)` | whether the manager executed the phase or skipped it |

For anything else, the hosts carry a live `rig` connection: `host.ExecOutput`, `host.ReadFile`, `host.FileExist`, and `host.Configurer` are all available and are how most assertions are written.

## Phases that are expected to skip

Some phases legitimately do nothing on this cluster -- SELinux where the runtime exposes none, fapolicyd when no host runs it, config sync when nothing has drifted. Do not write a test that asserts nothing; assert the *gate* instead. Compute what the hosts report, then require that `ran(p)` matches it:

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

This tests the routing, which is the part most likely to break, and it keeps working when the cluster gains a host that does trip the gate. `21_prepare_selinux_test.go` and `22_prepare_fapolicyd_test.go` are the worked examples.

`ran(p)` is only meaningful *after* `s.runPhase`, because the manager calls `Prepare` first and most gates are computed there.

## Phases that route on the OS family

The cluster runs three OS families on purpose. Nine of the machines join the cluster -- three controllers and six workers, each role split between Ubuntu (`kc0`, `kc1`, `kw0`-`kw2`) and Fedora (`kcf0`, `kwf0`-`kwf2`) -- and a tenth, `kwa0`, runs Alpine. A phase that treats Enterprise Linux differently from Debian must be tested on both sides, not just on the side it claims.

Derive the host set from the same filters the phase uses, so the test cannot disagree with it:

```go
enterprise := s.harness.hosts().Filter(utils.FilterEnterpriseLinux)
debian := s.harness.hosts().Filter(utils.FilterDebianLinux)
```

Then assert the split: the hosts the phase claims got what it promises, the hosts it does not came back unchanged.

Require the set you are testing to be non-empty. If someone collapses the cluster to one image, that turns a test which silently stopped covering anything into a failure that says so.

## The upload-only host

Alpine is neither Enterprise Linux nor Debian, so both of the filters above decline it. That is why it is in the inventory: it is the only host that reaches a phase's fallback branch as the fallback rather than as the path every host happens to take.

It cannot run rke2, which links against glibc, so it never joins the cluster. A phase that installs, starts, configures or queries the engine must not see it at all: those hosts have to come off the manager before the first such phase runs, and `s.harness.uploadOnly()` is what identifies them.

Do not "fix" a phase that fails on Alpine by skipping the host in the assertion. The phases select their own hosts in `Prepare`, with no OS gate in most cases, so the phase will still have run against it and spent its retry budget there. If a phase genuinely cannot run on a host, that host must not be on the manager when the phase runs.

`s.harness.engineWorkers()` is `workers()` without those hosts, for a test that runs while they are still on the manager but is only meaningful for the hosts that join.

## Prerequisites and running it

The suite needs Docker and a real distro package. It needs no prebuilt binary -- every step calls the cargoship packages directly, so a bare checkout is enough.

```console
$ go test -mod=vendor -count=1 -v -timeout=40m ./src/test/e2e/cluster/...
```

Or through mage, which also clears leftover containers from a run that was killed before teardown. Unlike the other e2e mage targets, it builds nothing first:

```console
$ mage test:endToEndCluster
```

`-short` skips the whole suite. `TestMain` deletes the bootloose cluster on the way out even when tests fail.

### In CI

`.github/workflows/e2e-cluster.yaml` runs it, on its own trigger and separate from `e2e.yaml`, which is what runs on every pull request. This one does not: it provisions ten containers, which is most of a hosted runner. It runs when triggered by hand from the Actions tab, and automatically on a pull request labelled `e2e-cluster`. Add that label to a PR touching `src/pkg/phase`, `src/pkg/action` or the inventory handling; the trigger listens for `labeled`, so labelling an open PR starts a run without needing a push.

The workflow has no build step and takes no artifact from `e2e.yaml`. Nothing in the suite runs a binary: `Test_00_CreatePackage` calls `distro.Create`, and the prepare step calls `action.NewPrepare`. That is what makes the two workflows independent, which is the point of the split.

The job frees disk before it starts, because ten containers and the images they unpack do not fit in what a hosted runner leaves free. If it fails with hosts that never come back, check the diagnostics step for a full disk or an OOM kill before reading the phase failure as a real one.

## Things that will cost you time

*   **You cannot run one phase test on its own.** `-run 'TestClusterPhases/apply/Test_25_ModifyHosts'` fails: the hosts were never connected, because `Test_07_Connect` and `Test_09_DetectOS` are earlier methods that `-run` filtered out. Run the whole suite, or accept that reproducing one phase means reproducing its predecessors.
*   **The first failure stops the rest.** `SetupTest` skips every remaining step once one has failed, so the output names one phase rather than all of them. If the first failure looks like a symptom, it is the cause -- everything after it was skipped, not passed.
*   **Constants the phase package keeps unexported.** Where a path is not exported, the test file redeclares it with a comment saying where it came from (`sysctlConfPath` in `20_prepare_host_test.go`). Prefer exporting from `phase` when the constant is genuinely part of the contract; duplicate it, with the comment, when it is not.
*   **Ten containers is a lot of memory.** That is the reason this suite is not part of the default `go test ./...` path.
