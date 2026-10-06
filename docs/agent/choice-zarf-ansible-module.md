# Why the zarf Ansible modules are wrapper binaries rather than zarf itself

[ZEP-0072](https://github.com/zarf-dev/proposals/pull/73) proposes native Ansible module support for zarf: the `zarf` binary dispatches on `argv[0]`, so a `zarf_init` symlink to `/usr/bin/zarf` is an Ansible module, with a thin Python action plugin alongside it. That is the design `colonel_byte.cargoship` already runs, and [choice-ansible-module](choice-ansible-module.md) is the record of why.

This repository holds a proof of that proposal for the two commands that converge a cluster, `zarf init` and `zarf package deploy`: the collection in `ansible/colonel_byte/zarf/`, the module logic in `internal/zarfmod/`, and the module files built from `cmd/zarf/init/` and `cmd/zarf/deploy/`. The collection also ships read-only modules, which are built as plain action plugins rather than as wrapper binaries and are not part of this proof -- see [choice-zarf-info-modules](choice-zarf-info-modules.md). It is a proof of concept and is meant to be removed once the proposal is resolved -- see [choice-zarf-collection-removal](choice-zarf-collection-removal.md). The proof diverges from the proposal in exactly one place, and that divergence is the point of this note.

## The divergence: the modules are not the zarf binary

A proof built here cannot put dispatch inside zarf, because zarf is not built here. So each module file is a separate, small binary that renders the zarf command line, runs the installed zarf as a child process, follows the deployment, and reports one JSON result. Three things follow from that, and all three are costs the real implementation would not pay.

**The flag surface is a copy rather than a reference.** Cargoship's modules render flags that are checked against the live cobra command by `TestModuleArgsParse`. The wrapper has no command tree to check against, so `internal/zarfmod/init.go` spells the flags out and `TestInitArgsAreKnownFlags` holds them against `internal/zarfmod/testdata/zarf-init-flags.txt`, a recorded list. That test catches a flag the module renders and the recorded zarf does not accept; it cannot catch a zarf release that renamed one without the list being refreshed. Inside zarf, this whole mechanism collapses into reading the real command.

**Component progress is read out of a log stream.** ZEP-0072 has the deployer emit heartbeats, where the component list and the phase boundaries are already known. The wrapper has neither, so `internal/zarfmod/progress.go` reads component boundaries back out of zarf's log output -- which is why the module renders `--log-format json` by default, and why the total component count is reported as `0` (an honest unknown) unless the operator named `--components`. This is the first file to delete if dispatch ever moves into zarf.

**`changedSignal` can never be `complete`.** Zarf reports which components it deployed, not whether a component found the cluster already in the state it wanted. A wrapper reading that stream cannot know more than the stream says, so a successful run reports `changed: true`, `changedSignal: partial`, and every component named in `changedUndeclared`. The proposal's `complete` signal needs the deployer, not the log.

## What the proof does keep

Everything else in the proposal is implemented as described: `argv[0]` dispatch with a `ZARF_ANSIBLE_MODULE` override, the WANT_JSON arguments file, the stdout guard that duplicates fd 1 and points `os.Stdout` at stderr, exit 0 with failure reported inside the JSON, check mode answered as `skipped` because `zarf init` has no dry run, and the heartbeat status file with a polling monitor thread in the action plugin.

Two of the proposal's open questions are answered here rather than deferred, because a proof has to pick something:

- **Argument delivery.** Both options are implemented. The action plugin pipes the parameters on stdin when the connection can carry them, which is the proposal's leaning and keeps registry and git credentials off disk; it falls back to a `0600` file in a `0700` directory when the connection cannot, which is the proposal's Option A. The wrapper reads both, so the fallback is a different wire and not a different contract.
- **Process cancellation.** The wrapper cancels its context on the first signal, which kills the zarf child, and exits on the second. That is the proposal's leaning -- a partial initialisation a later run reconciles beats one still mutating a cluster nobody is watching -- without the in-flight rollback the proposal leaves open.

## Why there are two modules

`zarf init` alone cannot be proved against a cluster whose distribution ships no storage provider, and that is not a quirk of the test environment -- it is the case the [`init-local-path` uds-bundle](https://github.com/colonel-byte/uds-bundles/blob/main/upstream/init-local-path/uds-bundle.yaml) exists to handle. Zarf's registry claims a 20Gi PVC during the seed phase. With no `StorageClass` the claim stays `Pending` until the Helm install times out, and disabling the claim does not help: the registry chart then renders no storage configuration at all and the container exits with `no storage configuration provided`. Both were measured rather than reasoned about, on a k3d cluster from that repository's own config.

What does work is deploying the storage provider package first with `--connected`, which pushes no images and so needs no registry, and so needs no initialised cluster. That makes `zarf package deploy` a prerequisite of `zarf init` rather than a follow-on, which is why the proof carries both actions and why `test/e2e/zarf` walks them in both orders -- the bundle's own order on a cluster that has k3s's StorageClass, and provider-first on one that has none.

Two findings from that suite are worth keeping even if everything else here goes away:

- **`--verify` must be rendered as `--verify=always`.** It is a flag with a no-argument default, so pflag reads it the way it reads a boolean: the space form leaves `always` as a positional argument and zarf rejects the command for having one too many. `command.valueFlag` exists for exactly this, and `--registry-mode` does not need it.
- **Registry proxy mode is what makes a local cluster usable.** Zarf's default nodeport mode pushes every image through a Kubernetes NodePort, and on k3d that push times out and retries for minutes. `registry_mode: proxy` puts a DaemonSet on a host port instead and the same initialisation finishes in about a minute.

## What the proof does not wire up

The reference pages are generated the same way the cargoship collection's are: `ansibleCollections` in `magefiles/pkg/gen/docs/ansible.go` lists both collections, and this one's pages land under `docs/ansible/zarf/` because the hand-written overviews that introduce them cannot share a filename with the cargoship collection's. The drift between a Go parameter struct and its Python `DOCUMENTATION` block is held by `internal/zarfmod/plugindocs_test.go`, which is the zarf side of `internal/ansiblemod/plugindocs_test.go`.

The end-to-end suite runs against two k3d clusters. One is [`k3d-config.yaml` in colonel-byte/uds-bundles](https://github.com/colonel-byte/uds-bundles/blob/main/k3d-config.yaml), copied verbatim, so that what the modules initialise is the cluster shape that repository already deploys onto; the other is the same file with k3s local-storage left enabled, which is what lets a walk begin with init. Both fixtures carry headers saying which walk they serve and why.
