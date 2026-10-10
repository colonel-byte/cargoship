# TODO: unit tests for the option-gated branches in `pkg/action`

This is a punch list, not a decision record like the `choice-*.md` files beside it. It exists because a coverage review of the repo flagged `pkg/action` (`apply.go`, `prepare.go`, `reset.go`, `kube-config.go`, `engine_config_sync.go`) at 0% under `go test -short`, and the obvious response -- write unit tests for the whole package -- turned out to be the wrong scope once `test/e2e/cluster/cluster_lifecycle_test.go` was checked. Read that file's comments before touching this list; they already say more about the intended coverage boundary than this document repeats.

## What is already covered, and why more of the same is not the ask

`cluster_lifecycle_test.go` calls `action.NewPrepare`, `action.NewApply`, `action.NewReset`, and `action.NewKubeConfig` directly -- not through the CLI -- against a real bootloose cluster, and asserts the cluster actually comes up healthy afterward. That is strictly stronger evidence for "does this action wire the right phases together" than a unit test asserting a phase list by name, because it also proves the phases it wires do the right thing together on real hosts. The comment on `Test_ZZ2_ApplyIsIdempotent` says this outright: *"the phase tests build each phase themselves... this is the only step that exercises action.NewApply's own wiring."* Duplicating that with a mocked-out `phase.Manager` would not close a correctness gap; it would just re-assert something already proven, at a lower bar of evidence.

**Partly done since this was written.** `pkg/action/apply_test.go` now exists. `newTestApply` builds an apply action the way a command does and `TestApplyChecksRunBeforeTheLock`, `TestApplyRefusesADowngradeBeforeTheLock` and `TestApplyPassesAllowDowngrade` assert over the assembled phase list without running a phase or touching a host. That settles steps 3 and 5 below -- the seam the list calls the real blocker turned out to exist, so no `choice-*.md` is owed and no interface had to be introduced. `apply.go` is no longer at 0% under `-short`; `prepare.go`, `reset.go`, `kube-config.go` and `engine_config_sync.go` still are, and the option combinations enumerated below are still unvaried. Follow `apply_test.go`'s pattern for the rest.

The `0%` figure from `go test -short` is consequently misleading on its own. It does not mean `pkg/action` is untested -- it means `pkg/action` has no coverage under the *fast* suite, because its only exercise lives in `mage test:endToEndCluster`, which needs Docker and is skipped under `-short`. `go test -short` measures the wrong thing here; a package can legitimately have all its correctness coverage in the cluster suite and still show 0% in the fast run. Don't let a future coverage pass flag this package again without checking `test/e2e/cluster` first, the way this document had to.

## The actual gap: one option combination per action, not the whole package

Every action in the lifecycle test runs with a single, fixed set of options:

- `Test_01_Prepare` — `ModifyHosts`, `ModifyFirewall`, `ModifyModules` all `true`. The `false` path for each is never run.
- `Test_ZZ2_ApplyIsIdempotent` — `ModifyHosts`, `ModifyFirewall`, `UpdateKubeConfig`, `LabelNodes` all `true`. `DisableDowngradeCheck` is never set to `true`, so the downgrade-check phase is never *omitted* by a test, only ever included.
- `Test_1_Reset` — `NoWait: true, NoDrain: true` always. The wait-for-drain and actual-drain branches never run in this suite.
- `engine_config_sync.go`'s action has no call site anywhere in `test/e2e/cluster`. That grep has now been run, and the answer is a split one: the *phases* are covered -- `71_engine_config_sync_controller_test.go` and `72_engine_config_sync_worker_test.go` assert `EngineConfigSyncController` and `EngineConfigSyncWorker` across the apply, join and upgrade walks -- but `action.NewEngineConfigSync`, which wires them together, is called only from `cmd/install_engine_config_sync.go`. So its wiring has no coverage of any kind, fast or slow, and it is the first thing on this list.

So the gap is narrow and specific: the conditional wiring inside each action -- "if this flag is set, add this phase; if not, don't" -- is proven for exactly one combination per action, and the untested combinations fail silently. A regression in, say, the `DisableDowngradeCheck` branch of `apply.go` would not be caught by the cluster suite as it is currently written, because that suite never sets the flag the other way.

## What to actually do about it

1. Close the `engine_config_sync` gap. The grep above has been run and the gap is real: the phases are covered, `action.NewEngineConfigSync`'s own wiring is not.
2. Read `pkg/action/apply.go`, `prepare.go`, `reset.go` end to end and enumerate every option field that changes which phases get added, not just the ones named above from a first pass.
3. Write narrow, table-driven unit tests per action, following `newTestApply` in `pkg/action/apply_test.go`: one table entry per option flag, asserting the phase list differs only in the way that flag controls, for every flag identified in step 2 -- not the whole option struct at once, and not by asserting the actions produce a working cluster, which is what the e2e suite is for.

Do not expand this into a general push for `pkg/action` coverage. The scope is the option-gated branches the cluster suite doesn't already vary, nothing more.
