# e2e-cluster.yaml

**Triggers:** manual dispatch, pull requests (opened/synchronize/reopened, excluding doc-only path changes).

Runs the "staging" half of the multi-machine end-to-end suite (`test/e2e/cluster`): it provisions a bootloose cluster of Docker machines, uploads the cargoship package, and renders the engine config on every OS family - then stops before actually starting the engine anywhere.

This is intentionally split out from `e2e.yaml` into its own workflow so its trigger, concurrency group, and time budget are independent of the checks required on every push. It calls the cargoship packages directly (`distro.Create`, `action.NewPrepare`/`action.NewApply`) rather than a built binary, so it needs no build step.

The other half - actually installing k3s and bringing the cluster fully up - is **not** run in CI. It isn't reliable enough on hosted runners to be a required check (even after switching from a three-controller RKE2/etcd topology to single-controller k3s to remove quorum timeouts). That half still exists and runs locally via `mage test:endToEndCluster`. See `docs/dev/e2e-phase-tests.md` and `docs/agent/choice-e2e-stage-split.md`.

On failure, node diagnostics (disk, memory, container logs) are collected and uploaded as an artifact, and leftover bootloose containers are always cleaned up.
