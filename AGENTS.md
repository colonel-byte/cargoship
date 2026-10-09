# Working in this repository

Cargoship is a Go CLI for building offline Kubernetes distro packages (K3s, RKE2) and for running the whole cluster lifecycle - `prepare`, `apply`, `kube-config`, `reset` - against target hosts over SSH. It also ships as an Ansible collection, `colonel_byte.cargoship`, so the same phases run as playbook tasks. See [`README.md`](README.md) for the full pitch, and [`docs/dev/e2e-tests.md`](docs/dev/e2e-tests.md) / [`docs/dev/e2e-phase-tests.md`](docs/dev/e2e-phase-tests.md) for how the phase model is exercised end to end. An OpenTofu provider that drives the same phases is taking shape at `cmd/terraform-provider-cargoship`; see [`docs/dev/tofu-provider.md`](docs/dev/tofu-provider.md) for how to run it.

It runs from a management node, not on the hosts it manages: cargoship opens the SSH connections itself, and the management node is typically air-gapped, staged with everything it needs (including a vendored `vendor/` tree) ahead of time. Nothing is installed on the target hosts beyond what a phase explicitly uploads.

## Layout

| Path                              | What lives there                                                                                                                                                                                                        |
| --------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `cmd/`                            | Cobra commands, and a `main` package per released binary: `cmd/cargoship` is the CLI, `cmd/terraform-provider-cargoship` the OpenTofu provider. The root `main.go` is a `go run .` convenience; see its package comment |
| `pkg/`                            | Packages usable from anywhere in the module                                                                                                                                                                             |
| `internal/`                       | Go packages private to this module - `riglogger`, `tofuprovider`, `ansiblemod`, `ansibleinv`, `clustercfg`, `cfg`, `dns`, `heartbeat`, `logging`, `split`                                                               |
| `fuzz/`                           | Fuzz targets - see [`fuzz/AGENTS.md`](fuzz/AGENTS.md)                                                                                                                                                                   |
| `config/lang/`                    | User-facing command strings - see [`config/lang/AGENTS.md`](config/lang/AGENTS.md)                                                                                                                                      |
| `test/e2e/`                       | The end-to-end suites: `cluster/` (needs a bootloose cluster), `noncluster/` (misc/package commands, plus `testdata/` fixtures), and `zarf/` (needs k3d, zarf and ansible-playbook)                                     |
| `magefiles/`                      | The mage task runner's source - build, test, and generate targets. See [`magefiles/AGENTS.md`](magefiles/AGENTS.md) and [`docs/dev/mage.md`](docs/dev/mage.md)                                                          |
| `docs/`                           | The mdBook source. Nine subpaths are generated and must not be hand-edited - see [`docs/AGENTS.md`](docs/AGENTS.md)                                                                                                     |
| `docs/agent/choice-*.md`          | Records of non-obvious decisions and the tradeoffs behind them - read before reversing one                                                                                                                              |
| `ansible/colonel_byte/cargoship/` | The Ansible collection: action plugins, modules, roles                                                                                                                                                                  |
| `example/`                        | Generated example packages - see [`example/AGENTS.md`](example/AGENTS.md)                                                                                                                                               |
| `schema/`                         | Generated JSON schemas for the YAML configs, from `mage generate:schema`                                                                                                                                                |
| `thirdparty-src/`                 | Pinned upstream k3s/RKE2 source, pulled by `mage generate:pullEngineSource`                                                                                                                                             |

Several directories carry their own `AGENTS.md` with rules specific to that directory. Check the closest one before editing. Keep this table in sync when adding or removing one -- add a row for a new `AGENTS.md`, and remove its row when the file goes away.

| `AGENTS.md`                                                                            | Covers                                                                                                              |
| -------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| [`docs/AGENTS.md`](docs/AGENTS.md)                                                     | Markdown formatting, generated pages                                                                                |
| [`.github/AGENTS.md`](.github/AGENTS.md)                                               | Scorecard-safe workflow changes, PR description format                                                              |
| [`test/e2e/AGENTS.md`](test/e2e/AGENTS.md)                                             | Fixtures belong in files, not Go string constants                                                                   |
| [`example/AGENTS.md`](example/AGENTS.md)                                               | Generated versus hand-written                                                                                       |
| [`ansible/AGENTS.md`](ansible/AGENTS.md)                                               | Markdown/string formatting rules that feed generated docs                                                           |
| [`ansible/colonel_byte/cargoship/AGENTS.md`](ansible/colonel_byte/cargoship/AGENTS.md) | Scope of the Ansible collection: drives cargoship from the management node, does not configure fleet hosts directly |
| [`config/lang/AGENTS.md`](config/lang/AGENTS.md)                                       | Markdown/string formatting rules that feed generated docs                                                           |
| [`fuzz/AGENTS.md`](fuzz/AGENTS.md)                                                     | Don't name two `Fuzz*` functions where one is a prefix of the other                                                 |
| [`magefiles/AGENTS.md`](magefiles/AGENTS.md)                                           | Mage target layout and structure rules                                                                              |

`CLAUDE.md` in the repository root is a symlink to this file, so a Claude Code session started there loads these rules without being asked. That covers the root and nothing else: the directory-specific files in the table above are not symlinked, so a session whose working directory is `docs/`, `test/e2e/`, `fuzz/`, `example/`, `config/lang/`, or `ansible/` starts with none of their rules in context. The root is where a session normally starts, so this is usually moot - but do not treat "nothing was loaded for me" as "there are no rules here", and do not grep a rules file for the one thing you thought to ask about. Open the closest `AGENTS.md` and read it before writing anything in that directory.

## Building and testing

Build and test targets run through mage, not a Makefile. `magefiles/` itself does not build with a plain `go build` - it needs mage's special build tag - so drive it through the `mage` CLI or `go run ./magefiles/core <namespace>:<target>`. The latter form needs only the Go toolchain and works on a host with no `mage` binary installed; it's what CI and pre-commit hooks use.

```sh
mage build:binary                           # build for this host's OS/arch
mage test:endToEndNonCluster                # misc/package suites, no cluster needed
mage test:endToEndCluster                   # install suite, needs Docker + bootloose
go run ./magefiles/core generate:document   # regenerate docs/commands, docs/phases, docs/golang, docs/schema, docs/ansible, docs/tofu, docs/index.md, docs/security.md, docs/SUMMARY.md
```

See [`docs/dev/mage.md`](docs/dev/mage.md) for the full namespace reference (`Build`, `Dev`, `Test`, `Generate`) and what each target reads and writes.

Plain Go commands work for anything mage doesn't wrap - `go build ./...`, `go vet ./...`, `go test ./api/... ./cmd/... ./config/... ./fuzz/... ./internal/... ./pkg/... ./types/...` - but exclude `./magefiles/...` from those, since it fails a bare build for the reason above.

Never run `go test ./...` (or any of the paths above) without `-short`. Without it, the suite pulls in the full e2e tests, including the cluster suite that needs Docker + bootloose. Always pass `-short`, or use the mage targets above which scope things correctly.

## Keeping `.gitattributes` in sync with the generators

Everything a generator writes is marked `linguist-generated` in [`.gitattributes`](.gitattributes), which is what collapses it in a GitHub diff and keeps it out of the repository's language statistics. A generated file that is not marked shows up as hand-written work in every review, and a thousand-line regenerated page buries the twenty lines that were actually written.

So a change to what a generator produces is a change to `.gitattributes` as well. That means all three of:

- **a new generator, or a new output path** - add the path. `mage generate:examples` writes `values.yaml` and `values.schema.json` beside each `distro.yaml`, so all three are marked; a pattern covering only the first leaves the other two looking hand-written.
- **a generator that writes into a new subdirectory** - widen the pattern rather than adding a sibling entry, where the directory depth is the only difference. `docs/ansible/**/module_*.md` covers a second collection's pages under `docs/ansible/<collection>/` without another line.
- **a generator removed, or its output moved** - delete the entry. A pattern matching nothing is not an error anybody sees.

Patterns are grouped by the mage target that writes them, with the target named in a comment, so the file reads as a list of what each generator owns.

Hand-written files inside a generated tree stay unmarked, and there are several: `docs/ansible/<collection>/collection.md`, `modules.md` and `roles.md` are written by hand beside generated module and role pages, and `pkg/engineconfig/gen/` holds hand-written Go next to the generated `zz_*.go`. Mark the pattern the generator writes, not the directory it writes into.

Check a specific file rather than reasoning about the patterns:

```sh
git check-attr linguist-generated -- docs/ansible/zarf/module_init.md
```

## Keeping `README.md` current

[`README.md`](README.md) is the repository's front page and the pitch: what cargoship does, which distros it supports, how to install it, and one example per phase. It is also the source for the book's landing page - `magefiles/pkg/gen/docs/book.go` copies it to `docs/index.md` - so a stale README is stale in two places.

Treat it as part of the change, not a follow-up:

- **a feature lands** - add it where a reader would look for it. A new phase or command belongs in `Usage Examples` with a runnable snippet; a new distro in `Supported Distributions`; a new install channel in `Installation`; a new config file or schema in `Configuration and Schemas`.
- **a feature is removed or renamed** - delete or rename every mention, including the example that exercised it. A documented flag that no longer exists is worse than no documentation.
- **behaviour changes under an existing heading** - fix the prose and the snippet together. Commands shown in `README.md` are copied and pasted; they should work as written against the current build.

The generated command and phase reference under `docs/` comes from the code, so it corrects itself on `go run ./magefiles/core generate:document`. `README.md` does not - it is hand-written, and nothing fails if it drifts. After editing it, run that target so `docs/index.md` matches.

## Committing changes

Run `pre-commit run --all-files` before every commit and fix anything it flags. Several hooks in [`.pre-commit-config.yaml`](.pre-commit-config.yaml) rewrite files (`end-of-file-fixer`, `trailing-whitespace`, `keep-sorted`, `addlicense`, doc/schema generators) - re-stage after it runs. Don't skip hooks with `--no-verify` or `SKIP=`.

## Go test formatting

Do not condense struct literals in table-driven test cases onto single lines (such as `{name: "...", input: "...", want: "..."}`). Format each field on its own line within the struct literal to keep test cases legible and easy to review, diff, and edit.

```go
// Do not condense:
{name: "unconstrained release", release: "", want: true},

// Prefer multi-line:
{
	name:    "unconstrained release",
	release: "",
	want:    true,
},
```

## Writing style

Use `-` (hyphen), not `—` (em dash), wherever a hyphen reads fine.

## Commit messages

Keep commit messages to a short title only - no body paragraph explaining the change. End with a `Co-Authored-By` trailer.

```
test(fuzz): add fuzz targets for cfg, clustercfg, and identify source

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
```

## Pull requests

Before creating or updating a pull request description, read [`.github/pull_request_template.md`](.github/pull_request_template.md) and follow the format rules in [`.github/AGENTS.md`](.github/AGENTS.md#writing-a-pull-request-description).

## Tracking work in issues

Work here is tracked in issues on the main repository, [`colonel-byte/cargoship`](https://github.com/colonel-byte/cargoship). Before starting a task, check for a related issue - someone may already have filed it, or filed something that overlaps enough to change the approach:

```sh
gh issue list --repo colonel-byte/cargoship --search "<keywords>" --state all
```

If there is no issue covering the work, ask the person whether they would like one submitted before continuing. Don't open one unprompted, and don't treat a missing issue as a reason to stop - they may well say no.

When work is being broken up into a stacked PR, the issue structure should mirror the stack: the parent issue describes the whole change, and each layer of the stack gets a subissue created off that parent. Link each PR in the stack to its own subissue, not to the parent.

Only the top-most PR in the stack closes the parent issue - each lower PR closes its own subissue and nothing more, so the parent stays open until the whole change has landed. Don't move that closing reference to an earlier PR unless the operator asks for it.

## Quirks worth knowing

### Relative-path fixture constants don't move themselves

Test files and YAML fixtures across the repo hand-encode `../` depth to reach the repository root or a shared `testdata/` directory (e.g. `internal/ansiblemod/collection_test.go`'s `collectionRoot`, `cmd/misc_validate_test.go`'s `valuesSchemaPackageDir`, the `$schema=` headers in `test/e2e/noncluster/testdata/*.yaml`). Moving either endpoint of one of these paths changes the required `../` count by exactly the number of levels moved, and nothing catches a wrong count except actually running the test - `go vet` only compiles, it doesn't execute. Recompute depth from the final location of both the file and its target, not from an intermediate state, if the move happens in more than one step.

### `src/test/` had a special exemption; `test/` does not

OpenSSF Scorecard's `isTestdataFile` logic excludes anything under a `src/test/` or `testdata/` prefix from every check, by path convention. That's why fuzz targets never lived under that prefix - being excluded from Scorecard would zero the Fuzzing check rather than help it. It also means the e2e suite lived under `src/test/` for a while getting a blanket exclusion it didn't need; now that it's `test/`, it doesn't have one. Don't rely on a `test/` or `testdata/`-adjacent path to hide something from Scorecard - check `isTestdataFile` in `checks/fileparser/listing.go` upstream if it matters.

### `docs/agent/choice-*.md` are constraints, not history

When a decision looks reversible from the code alone - vendoring `vendor/` despite the size, keeping a Scorecard check capped instead of forcing it to 10, pinning Ansible collections by hand instead of Renovate - check `docs/agent/` first. These are usually the record of a tradeoff already made on purpose, not an oversight waiting to be cleaned up.
