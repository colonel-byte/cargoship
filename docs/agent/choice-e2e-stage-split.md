# Why the cluster e2e suite splits into a staging half and an engine half

The cluster suite (`src/test/e2e/cluster`) walks the apply phase list one phase at a time against a bootloose cluster, as [choice-phase-e2e-tests](choice-phase-e2e-tests.md) describes. That suite is now two runs rather than one: with `CARGOSHIP_E2E_STAGE_ONLY` set it provisions five machines instead of ten and stops asserting at the boundary `phase/60_configure_engine.go` draws, and without it nothing changes. This document records why, because the split looks like an arbitrary line through the middle of a test and the line was not arbitrary.

## What forced it

The suite was label-gated into CI on a hosted `ubuntu-latest` runner and did not pass there. Two things were wrong, and only the first was ours.

`phase/55_files_common.go` marked a host as claimed from the host list rather than from what it had uploaded. An rke2 package ships no `.deb`, so the APT phase claimed all five Debian hosts, uploaded nothing to them, and registered an `apt-get install` with an empty package list -- which succeeds. `phase/59_bin_install.go`, the catch-all that exists precisely for a distro that ships neither RPMs nor debs, then filtered those hosts out as already claimed, and they reached the initialize phases with no engine on them at all. That is a real product bug, it is fixed, and the local run had never caught it because the failure needs a Debian host and a package with no debs to meet.

Underneath it was the blocker that is not fixable in the product. rke2 runs its own containerd, whose default snapshotter is overlayfs, and overlayfs refuses an upperdir that is itself on an overlay mount -- `EINVAL`, reported as `"overlayfs" snapshotter cannot be enabled for "/var/lib/rancher/rke2/agent/containerd"`. A hosted runner's Docker uses the `overlay2` storage driver, so every node container's root filesystem is an overlay mount and the snapshotter has nowhere to stack. The engine never reaches ready.

The timings from that run are what shaped the rest. Everything through `Test_60_ConfigureEngine` -- fifteen phases, ten machines -- took 188 seconds on the runner and passed. `Test_61_InitializeControllers` alone took 1236 seconds and then failed. The suite is not uniformly expensive; almost all of its cost and all of its environmental risk sit on one side of one line.

## The seam was already in the code

`Test_60_ConfigureEngine` calls `harness.dropUploadOnlyHosts()`. The Alpine machine that exists to make `phase/59` testable as the fallback it is cannot run rke2 -- rke2 links against glibc and Alpine is musl -- so 60 is where it leaves the walk. The phases up to and including 60 stage files and render configuration; every phase after it installs, starts or queries the engine. That distinction was already written down, already load-bearing, and already the reason one host stops there.

So the split reuses it rather than inventing a second boundary. A stage-only run stops asserting exactly where the Alpine host stops participating, and the sentence that justifies both is the same sentence.

Drawing the line by wall-clock instead -- "skip the phases that take more than a minute" -- was rejected on the obvious grounds that it encodes a measurement rather than a property, and would have to be re-drawn every time a phase got faster or slower.

## An environment variable, not a build tag

The gate is `stageOnlyEnvVar`, read through `strconv.ParseBool`, under the `CARGOSHIP_E2E_` prefix the suite already uses for its other knobs. Build tags were the alternative and were rejected twice over: a tagged file is invisible to `go vet` and the linter unless the tag is set, so the gated half rots silently, and the suite would need two `go test` invocations with different tags to cover what one binary covers now. An environment variable is one build, one binary, and the same code compiled either way.

It is a function rather than a package variable so that it reads the environment when asked rather than at package init, which is what lets a caller set it in process -- `mage test:endToEndClusterStage` does exactly that. `CARGOSHIP_E2E_UPGRADE`, the opt-in for the upgrade walk, is read the same way for the same reasons; the two gates are independent, and a stage-only run has nothing to upgrade.

## Two granularities of skip

`ApplyPhaseSuite` is skipped per method. Each of `Test_61` through `Test_81` begins with `requireEngine()`, which skips that one step. The walk still runs to the end and still tears the cluster down, which is what lets the phases on the far side of the engine half keep running.

The join and upgrade walks are skipped whole, in `TestClusterPhases`. Each starts from the cluster the apply walk installed -- there is nothing to join or upgrade when nothing was started -- so gating them method by method would produce two suites of uniformly skipped tests, and a machine provisioned and a phase list walked to reach them. The apply walk is the only one with anything left to say.

## The phases after the engine still run

`Test_91_Lock`, `Test_92_Unlock` and `Test_99_Disconnect` carry no `requireEngine()`, and that is deliberate rather than an oversight. The lock is a file on each host holding the instance ID of the process that took it; unlock removes it; disconnect clears the staged binary paths and drops the SSH connections. All three need a connected host and none needs an engine, so all three run on a stage-only walk and fail it if they regress.

That matters more than it looks. A lock left behind delays the next run by thirty seconds on every node, and a stage-only walk that took the lock and skipped the release would do exactly that to the run after it. The three tests are commented to say they are ungated on purpose, so that a later change adding `requireEngine()` to everything after 60 has to argue with a comment first.

## Five machines, not three and not ten

The full walk provisions ten machines for reasons [choice-phase-e2e-tests](choice-phase-e2e-tests.md) sets out: three controllers, six workers, both roles split across Ubuntu and Fedora, plus the Alpine upload-only node. A stage-only run provisions five -- one Ubuntu controller, one Fedora controller, one Ubuntu worker, one Fedora worker, and the Alpine node.

Three was the obvious target: one machine per OS family, which is the axis the phase list most visibly routes on. It is one short. `APTUploadFiles.Prepare` filters both host sets by family *and* by role, and `UploadFilesCommon.Run` uploads once per role with that role's file list:

```go
p.control = p.control.Filter(utils.FilterEngineAlreadyPopulated).Filter(utils.FilterDebianLinux)
p.workers = p.workers.Filter(utils.FilterEngineAlreadyPopulated).Filter(utils.FilterDebianLinux)
```

With one machine per family each family has to take a single role, so one family's controller set and the other's worker set are empty and half the cells the upload phases branch on go unvisited. Those cells are not hypothetical: the host-claiming bug above lives on exactly this axis. Five is the smallest inventory that populates every family-by-role cell, and it is still half the containers, half the uploads and half the disk of the full one.

Nothing else the staging phases do is sensitive to machine count, which was checked rather than assumed. The upload batching in `parallelDoUpload` is bounded by `applyConcurrency`, which the suite sets to 300, so all ten hosts already go in a single batch and no count below that changes anything. The `WorkerConcurrent` batching that the ten-node cluster is partly justified by is reached only in the initialize and upgrade phases, which a stage-only run does not reach.

The host counts the suite asserts on -- `inventoryHostCount`, `uploadOnlyCount`, `clusterControllers`, `clusterWorkers` -- were constants pinned to the ten-machine cluster. They are now derived from whichever config the run selected, by `countsFor`, reading the same `kc`/`kw`/`kwa` name prefixes that `renderClusterInventory` maps to roles. Writing a second set of constants for the second inventory was the alternative, and it is the arrangement where the two silently disagree after someone edits one.

## A volume over the engine's data directory

Every bootloose machine now mounts an anonymous Docker volume at `/var/lib/rancher`. A volume is not part of the container's root filesystem -- Docker backs it with a directory on whatever filesystem holds `/var/lib/docker` -- so containerd's snapshotter gets a plain filesystem and mounts normally. This is the same arrangement `kind`'s node image makes with its `VOLUME` declaration, for the same reason.

Anonymous rather than named, on purpose. A named volume would carry one run's engine state into the next, and every walk in this suite assumes it starts from machines with no engine on them. The cost is that `docker rm -f` does not reap anonymous volumes, so both `stopBootlooseContainers` in `magefiles/test-e2e-cluster.go` and the workflow's cleanup steps pass `-v`.

Configuring rke2 to use the `native` snapshotter instead was the alternative. It was rejected because it changes what is under test: the suite would then be proving that rke2 installs correctly in a configuration no real deployment uses, and the overlayfs snapshotter is the one that ships.

This fix is unverified. It addresses the exact error in the logs and it is the standard remedy, but confirming it costs a full CI run, and the split is what makes shipping it unconfirmed reasonable -- if it does not work, the stage job still covers the phases that do not depend on it.

## Two CI jobs rather than one matrix

`e2e-cluster-stage` and `e2e-cluster` are separate jobs with duplicated steps. A matrix over a `mode` dimension was the alternative and would have removed the duplication, at the price of turning every field the two jobs disagree on into an expression over the matrix value. They disagree on the job timeout, the test timeout, the environment block and the disk-space rationale, and the timeouts differ by a factor of two because that difference is the entire point of having two jobs. Six duplicated boilerplate steps read better than four conditionals.

Both jobs sit behind the same `e2e-cluster` label for now. The point of the stage job is that it is cheap enough to run unconditionally, and the intent is to drop its `if:` once a few runs show what it actually costs and how steady it is. Landing it unconditional immediately was rejected for one reason: an unproven job in every pull request's path, if it turns out to be flaky, teaches people to ignore a red check, and that is expensive to undo. The flip is one line when the data supports it.

## What this does not do

A stage-only run does not tell you that rke2 installs. It covers connection, OS detection, fact gathering, host validation, the prepare phases, SELinux, fapolicyd, `/etc/hosts`, the firewall, all four upload phases, the engine config render, and the lock and disconnect phases at the end -- which is most of the phase list and the part a change to `src/pkg/phase` is most likely to break. It says nothing about whether the cluster comes up, and the full walk remains the only thing that does.
