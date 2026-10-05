# The zarf collection is a proof of concept, and is meant to be removed

`ansible/colonel_byte/zarf`, `internal/zarfmod`, `cmd/zarf/`, and `test/e2e/zarf` exist to prove [ZEP-0072](https://github.com/zarf-dev/proposals/pull/73), which proposes native Ansible module support for zarf. They are not part of what cargoship does. Cargoship builds and installs offline Kubernetes distro packages and runs a cluster lifecycle over SSH; nothing in that needs a zarf module, and no cargoship command calls into any of this.

They live here because the pattern the proposal describes is the one `colonel_byte.cargoship` already runs -- see [choice-ansible-module](choice-ansible-module.md) -- so the cheapest place to find out whether the pattern holds for zarf was beside the implementation it copies. That was the whole reason, and it expires.

This note exists so that whoever removes it does not have to work out whether it was load-bearing first.

## When to remove it

Any one of these is enough:

- **ZEP-0072 is accepted and implemented in zarf.** Then the real thing exists, dispatch lives inside the `zarf` binary, and keeping wrappers that shell out to zarf means maintaining a worse copy of a shipped feature.
- **ZEP-0072 is rejected, or lands somewhere else.** The maintainers' own feedback on the pull request raised an isolated experimental repository and a plugin architecture as alternatives. If either is where this goes, the proof belongs in that repository rather than this one.
- **It stops being maintained here.** The flag surfaces are recorded lists rather than reads of zarf's command tree (see [choice-zarf-ansible-module](choice-zarf-ansible-module.md)), and the e2e suite pulls packages from two registries. The first time a zarf release breaks it and nobody is willing to refresh it, delete it rather than skip it.

Nothing about cargoship's own roadmap forces the removal, and nothing breaks while it stays. It is dead weight rather than a hazard: a release artifact administrators carry through an airlock and never install, and a test suite with prerequisites most contributors do not have.

## What to remove

| Path                                                  | What it is                                                                         |
| ----------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `ansible/colonel_byte/zarf/`                          | The collection: action plugins, the shared projection module, the example playbook |
| `internal/zarfmod/`                                   | Dispatch, the WANT_JSON intake, the response, the stdout guard, the heartbeat      |
| `cmd/zarf/init/`, `cmd/zarf/deploy/`                  | The two module file entry points                                                   |
| `test/e2e/zarf/`                                      | The k3d suite, both k3d configs, both playbooks, the package signing key           |
| `docs/guides/zarf-ansible-module.md`                  | The user guide                                                                     |
| `docs/agent/choice-zarf-ansible-module.md`, this file | The decision records                                                               |

And these edits, each of which is the only thing in its file that mentions zarf-the-tool:

- `.goreleaser.yaml`: the `zarf_init` and `zarf_package_deploy` builds, the `zarf_modules` archive, the second `before` hook, the collection tree and two module symlinks under `nfpms[0].contents`, the two `bin/ansible/colonel_byte-zarf-*` globs under `release.extra_files`, and the two wrapper ids in `nfpms[0].ids`. Leave the `ids: [cargoship]` on the first archive: it is right either way, and without it the cargoship archive silently absorbs whatever builds exist.
- `containers/ansible/Dockerfile`: the `zarf` build stage, the `COPY --from=zarf` line, the `zarf version` check, the `/home/nonroot/.zarf-cache` directory, the `SHELL` line, and the whole smoke-check `RUN` block after it. The `cargoship version` check stays, and so does `/home/nonroot/.zarf` -- cargoship's own cache paths predate this. Dropping the stage takes about 200MB off the image.
- `magefiles/pkg/build/zarfmodules.go`, and in `magefiles/pkg/testrunner/testrunner.go` the `RunE2EZarf`, `K3dClusters` and `DeleteK3dClusters` functions, and in `magefiles/test.go` the `EndToEndZarf` and `CleanZarfClusters` targets.
- `hack/ansible-build-collection.sh`: the third positional argument and the namespace/name lookup can stay -- they are a generalisation that costs nothing and the cargoship path does not care -- or revert to the hardcoded collection. Do not leave the script accepting a collection directory that no longer exists while the before hook still names it.
- `docs/dev/mage.md`, `docs/dev/e2e-tests.md`, `docs/workflows/test-build-containers.md`, `AGENTS.md`'s layout table, and `.gitignore`'s `__pycache__` entry. The `.gitignore` entry should **stay**: it is right for the cargoship collection on its own.

Then `go run ./magefiles/core generate:document`, which rewrites `docs/SUMMARY.md`, and `pre-commit run --all-files`.

## What not to take with it

- **`internal/ansiblemod` and `ansible/colonel_byte/cargoship`.** The zarf side is modeled on them and shares no code with them. Nothing in `internal/zarfmod` is imported by anything outside itself and `cmd/zarf/`, and nothing in the cargoship collection refers to the zarf one.
- **`internal/heartbeat`.** It predates this work, is used by the cargoship phases, and the zarf side deliberately writes its own rather than reusing it.
- **The `zarf-dev/zarf` dependency in `go.mod`.** Cargoship depends on zarf as a library for package formats, architecture detection and OCI handling, and has since long before this. Removing the collection does not make it unused; `go mod tidy` will tell the truth about that on its own.

## Why this is a record rather than a TODO

The removal is a decision somebody should make deliberately, with the proposal's outcome in hand, not a chore to be swept up by whoever next tidies the repository. A `todo-` note would invite the second reading. What makes it safe to remove on short notice is the list above, and what makes it safe to leave is that none of it is reachable from cargoship's own code paths.
