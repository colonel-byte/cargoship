# Working in this repository

Cargoship is a Go CLI for building offline Kubernetes distro packages (K3s, RKE2) and for running the whole cluster lifecycle — `prepare`, `apply`, `kube-config`, `reset` — against target hosts over SSH. It also ships as an Ansible collection, `colonel_byte.cargoship`, so the same phases run as playbook tasks. See [`README.md`](README.md) for the full pitch, and [`docs/dev/e2e-tests.md`](docs/dev/e2e-tests.md) / [`docs/dev/e2e-phase-tests.md`](docs/dev/e2e-phase-tests.md) for how the phase model is exercised end to end.

It runs from a management node, not on the hosts it manages: cargoship opens the SSH connections itself, and the management node is typically air-gapped, staged with everything it needs (including a vendored `vendor/` tree) ahead of time. Nothing is installed on the target hosts beyond what a phase explicitly uploads.

## Layout

| Path                              | What lives there                                                                                                                                                                                   |
| --------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `src/`                            | The Go module's source: `cmd/` (Cobra commands), `pkg/` (packages usable from anywhere in `src/`), `internal/` (packages private to `src/`), `fuzz/`, `config/lang/` (user-facing command strings) |
| `internal/`                       | Go packages shared across the whole module — `src/`, `test/`, and each other — see the quirk below                                                                                                 |
| `test/e2e/`                       | The end-to-end suite: `cluster/` (needs a bootloose cluster) and `noncluster/` (misc/package commands, plus `testdata/` fixtures)                                                                  |
| `magefiles/`                      | The mage task runner's source — build, test, and generate targets. See [`docs/dev/mage.md`](docs/dev/mage.md)                                                                                      |
| `docs/`                           | The mdBook source. Six subpaths are generated and must not be hand-edited — see [`docs/AGENTS.md`](docs/AGENTS.md)                                                                                 |
| `docs/agent/choice-*.md`          | Records of non-obvious decisions and the tradeoffs behind them — read before reversing one                                                                                                         |
| `ansible/colonel_byte/cargoship/` | The Ansible collection: action plugins, modules, roles                                                                                                                                             |
| `example/`                        | Generated example packages — see [`example/AGENTS.md`](example/AGENTS.md)                                                                                                                          |
| `schema/`                         | Generated JSON schemas for the YAML configs, from `mage generate:schema`                                                                                                                           |
| `thirdparty-src/`                 | Pinned upstream k3s/RKE2 source, pulled by `mage generate:pullEngineSource`                                                                                                                        |

Several directories carry their own `AGENTS.md` with rules specific to that directory: [`docs/AGENTS.md`](docs/AGENTS.md) (markdown formatting, generated pages), [`.github/AGENTS.md`](.github/AGENTS.md) (Scorecard-safe workflow changes, PR description format), [`test/e2e/AGENTS.md`](test/e2e/AGENTS.md) (fixtures belong in files, not Go string constants), [`example/AGENTS.md`](example/AGENTS.md) (generated versus hand-written), [`ansible/AGENTS.md`](ansible/AGENTS.md) and [`src/config/lang/AGENTS.md`](src/config/lang/AGENTS.md) (markdown/string formatting rules that feed generated docs). Check the closest one before editing.

## Building and testing

Build and test targets run through mage, not a Makefile. `magefiles/` itself does not build with a plain `go build` — it needs mage's special build tag — so drive it through the `mage` CLI or `go run ./magefiles/core <namespace>:<target>`. The latter form needs only the Go toolchain and works on a host with no `mage` binary installed; it's what CI and pre-commit hooks use.

```sh
mage build:binary                           # build for this host's OS/arch
mage test:endToEndNonCluster                # misc/package suites, no cluster needed
mage test:endToEndCluster                   # install suite, needs Docker + bootloose
go run ./magefiles/core generate:document   # regenerate docs/commands, docs/phases, docs/ansible, docs/index.md, docs/security.md, docs/SUMMARY.md
```

See [`docs/dev/mage.md`](docs/dev/mage.md) for the full namespace reference (`Build`, `Dev`, `Test`, `Generate`) and what each target reads and writes.

Plain Go commands work for anything mage doesn't wrap — `go build ./...`, `go vet ./...`, `go test ./internal/... ./src/...` — but exclude `./magefiles/...` from those, since it fails a bare build for the reason above.

## Commit messages

Keep commit messages to a short title only — no body paragraph explaining the change. End with a `Co-Authored-By` trailer.

```
test(fuzz): add fuzz targets for cfg, clustercfg, and identify source

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
```

## Quirks worth knowing

### `internal/` visibility is scoped to its parent directory, not the whole module

A package under `<X>/internal/...` is importable only from packages rooted at `<X>` or below — that's the Go compiler's rule, not a convention. This repository has two `internal/` roots for exactly that reason: repo-root `internal/` (`riglogger`, `ansiblemod`, `ansibleinv`, `clustercfg`) is reachable from `src/`, `test/`, and each other, while `src/internal/` (`cfg`, `dns`, `heartbeat`, `logging`, `split`) is reachable only from within `src/`. Moving a directory across that boundary — or moving something that imports an `internal/` package across it — breaks the import silently at compile time, not at the point of the move. `go build` won't catch it either; only `go vet` (or a full `go test`) compiles the test files where these breaks tend to show up first.

### Relative-path fixture constants don't move themselves

Test files and YAML fixtures across the repo hand-encode `../` depth to reach the repository root or a shared `testdata/` directory (e.g. `internal/ansiblemod/collection_test.go`'s `collectionRoot`, `src/cmd/misc_validate_test.go`'s `valuesSchemaPackageDir`, the `$schema=` headers in `test/e2e/noncluster/testdata/*.yaml`). Moving either endpoint of one of these paths changes the required `../` count by exactly the number of levels moved, and nothing catches a wrong count except actually running the test — `go vet` only compiles, it doesn't execute. Recompute depth from the final location of both the file and its target, not from an intermediate state, if the move happens in more than one step.

### `src/test/` had a special exemption; `test/` does not

OpenSSF Scorecard's `isTestdataFile` logic excludes anything under a `src/test/` or `testdata/` prefix from every check, by path convention. That's why fuzz targets live in `src/fuzz/` rather than under that prefix — being excluded from Scorecard would zero the Fuzzing check rather than help it. It also means the e2e suite lived under `src/test/` for a while getting a blanket exclusion it didn't need; now that it's `test/`, it doesn't have one. Don't rely on a `test/` or `testdata/`-adjacent path to hide something from Scorecard — check `isTestdataFile` in `checks/fileparser/listing.go` upstream if it matters.

### `docs/agent/choice-*.md` are constraints, not history

When a decision looks reversible from the code alone — vendoring `vendor/` despite the size, keeping a Scorecard check capped instead of forcing it to 10, pinning Ansible collections by hand instead of Renovate — check `docs/agent/` first. These are usually the record of a tradeoff already made on purpose, not an oversight waiting to be cleaned up.
