# Working in this repository

Cargoship is a Go CLI for building offline Kubernetes distro packages (K3s, RKE2) and for running the whole cluster lifecycle - `prepare`, `apply`, `kube-config`, `reset` - against target hosts over SSH. It also ships as an Ansible collection, `colonel_byte.cargoship`, so the same phases run as playbook tasks. See [`README.md`](README.md) for the full pitch, and [`docs/dev/e2e-tests.md`](docs/dev/e2e-tests.md) / [`docs/dev/e2e-phase-tests.md`](docs/dev/e2e-phase-tests.md) for how the phase model is exercised end to end. An OpenTofu provider that drives the same phases is taking shape at `cmd/terraform-provider-cargoship`; see [`docs/dev/tofu-provider.md`](docs/dev/tofu-provider.md) for how to run it.

It runs from a management node, not on the hosts it manages: cargoship opens the SSH connections itself, and the management node is typically air-gapped, staged with everything it needs (including a vendored `vendor/` tree) ahead of time. Nothing is installed on the target hosts beyond what a phase explicitly uploads.

## Layout

| Path                              | What lives there                                                                                                                                                                                                                             |
| --------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `cmd/`                            | Cobra commands, and a `main` package per released binary: `cmd/cargoship` is the CLI, `cmd/terraform-provider-cargoship` the OpenTofu provider. The root `main.go` is a `go run .` convenience; see its package comment                      |
| `pkg/`                            | Packages usable from anywhere in the module                                                                                                                                                                                                  |
| `internal/`                       | Go packages private to this module - `riglogger`, `tofuprovider`, `ansiblemod`, `ansibleinv`, `clustercfg`, `cfg`, `dns`, `heartbeat`, `logging`                                                                                             |
| `fuzz/`                           | Fuzz targets - see [`fuzz/AGENTS.md`](fuzz/AGENTS.md)                                                                                                                                                                                        |
| `config/lang/`                    | User-facing command strings - see [`config/lang/AGENTS.md`](config/lang/AGENTS.md)                                                                                                                                                           |
| `test/e2e/`                       | The end-to-end suites: `cluster/` (needs a bootloose cluster), `noncluster/` (misc/package commands, plus `testdata/` fixtures), and `tofu/` (needs a bootloose cluster and a `tofu` binary)                                                 |
| `magefiles/`                      | The mage task runner's source - build, test, and generate targets. See [`magefiles/AGENTS.md`](magefiles/AGENTS.md) and [`docs/dev/mage.md`](docs/dev/mage.md)                                                                               |
| `docs/`                           | The mdBook source. Nine subpaths are generated and must not be hand-edited - see [`docs/AGENTS.md`](docs/AGENTS.md)                                                                                                                          |
| `docs/agent/choice-*.md`          | Records of non-obvious decisions and the tradeoffs behind them - read before reversing one                                                                                                                                                   |
| `ansible/colonel_byte/cargoship/` | The Ansible collection: action plugins, modules, roles                                                                                                                                                                                       |
| `example/`                        | Generated example packages - see [`example/AGENTS.md`](example/AGENTS.md)                                                                                                                                                                    |
| `schema/`                         | Generated JSON schemas for the YAML configs, from `mage generate:schema`                                                                                                                                                                     |
| `thirdparty-src/`                 | Pinned upstream k3s/RKE2 source, pulled by `mage generate:pullEngineSource`                                                                                                                                                                  |

Several directories carry their own `AGENTS.md` with rules specific to that directory. Check the closest one before editing. Keep this table in sync when adding or removing one -- add a row for a new `AGENTS.md`, and remove its row when the file goes away.

| `AGENTS.md`                                                                            | Covers                                                                                                              |
| -------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| [`docs/AGENTS.md`](docs/AGENTS.md)                                                     | Markdown formatting, generated pages                                                                                |
| [`.github/AGENTS.md`](.github/AGENTS.md)                                               | Scorecard-safe workflow changes, PR description format                                                              |
| [`test/AGENTS.md`](test/AGENTS.md)                                                     | What the three e2e backends do and do not cover, and which one to reach for                                         |
| [`test/e2e/AGENTS.md`](test/e2e/AGENTS.md)                                             | Fixtures belong in files, not Go string constants                                                                   |
| [`example/AGENTS.md`](example/AGENTS.md)                                               | Generated versus hand-written                                                                                       |
| [`ansible/AGENTS.md`](ansible/AGENTS.md)                                               | Markdown/string formatting rules that feed generated docs                                                           |
| [`ansible/colonel_byte/cargoship/AGENTS.md`](ansible/colonel_byte/cargoship/AGENTS.md) | Scope of the Ansible collection: drives cargoship from the management node, does not configure fleet hosts directly |
| [`config/lang/AGENTS.md`](config/lang/AGENTS.md)                                       | Markdown/string formatting rules that feed generated docs                                                           |
| [`fuzz/AGENTS.md`](fuzz/AGENTS.md)                                                     | Don't name two `Fuzz*` functions where one is a prefix of the other                                                 |
| [`magefiles/AGENTS.md`](magefiles/AGENTS.md)                                           | Mage target layout and structure rules; how pinned versions, digests, and checksums are kept current                |

`CLAUDE.md` in the repository root is a symlink to this file, so a Claude Code session started there loads these rules without being asked. That covers the root and nothing else: the directory-specific files in the table above are not symlinked, so a session whose working directory is `docs/`, `test/`, `test/e2e/`, `fuzz/`, `example/`, `config/lang/`, or `ansible/` starts with none of their rules in context. The root is where a session normally starts, so this is usually moot - but do not treat "nothing was loaded for me" as "there are no rules here", and do not grep a rules file for the one thing you thought to ask about. Open the closest `AGENTS.md` and read it before writing anything in that directory.

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

### The binary has a size budget

`cargoship` is carried onto air-gapped networks by hand. It crosses on removable media, through a one-way diode, or over a link metered by the hour, and it is staged alongside the distro packages, the container images and everything else the management node needs before it is disconnected. Every mebibyte is one somebody physically moves, often repeatedly, often without a resumable transfer.

So the binary has a budget, and it is a design constraint rather than a nice-to-have:

| Size (MiB, `mage build:binary`, linux/amd64) | What it means                                                                                            |
| -------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| **50-60**                                    | The target. Comfortable to carry, and the range to aim for when weighing a dependency.                   |
| 60-100                                       | Acceptable, and worth a look. Something large is linked; find out what before adding more.               |
| **over 100**                                 | **Warn, and say so in the change.** Not a hard failure, but a number nobody should discover by accident. |

There is no enforced limit, and this is deliberate: a hard gate in CI would get raised the first time it blocked something urgent, and the useful behaviour is a human noticing rather than a threshold being edited. What is expected is that a change which moves the binary materially says so, with a measured before and after.

It was **100.71 MiB** as of `f3678071b` and is **39.48 MiB** now; [#846](https://github.com/colonel-byte/cargoship/issues/846) records where the 61 MiB went and what is left. That history is the argument for the budget: nothing in it was a deliberate decision to ship a 100 MiB binary. It was four dependency trees carried for features unreachable from an air-gapped host - containerd's image exporter, the docker-socket image pull, cosign's cloud KMS and CLI command structs, and the Kubernetes typed clientset - each arriving transitively, none visible from the code that pulled it in.

Which is the practical rule: **the cost of a dependency is what it links, not what you call.** One import of a helper package for four symbols pulled `docker/cli` and the Docker daemon client; one typed Kubernetes client registered all 59 API groups; one `actions.Run` call linked `kubectl` and `kustomize`. Check with `go list -deps ./cmd/cargoship` before adding an import, and read the section below before quoting any number.

Note the units. These are **MiB** - what `ls`, `du` and `ls -la | awk '{$5/1048576}'` report - not decimal megabytes. The difference is about 5% at this size, which is enough to argue past each other over.

### The cluster suite runs on three backends, and they cover different things

`mage test:endToEndCluster` provisions privileged containers with bootloose. `CARGOSHIP_E2E_BACKEND=microvm` provisions Rocky virtual machines instead, and is the only way the SELinux, fapolicyd and firewalld phases run as the paths they are rather than as gates that correctly do nothing. `CARGOSHIP_E2E_BACKEND=inventory` runs against hosts that already exist and provisions nothing.

They are not interchangeable, and a walk that passed on one says less than it looks about the others:

- the **container** backend carries several OS families, so it is what exercises the family-routed branches, and it reports no SELinux, runs no fapolicyd and has no firewall of its own
- the **VM** backend runs one distribution and cannot reach the cluster API from the host, so every step that talks to Kubernetes skips there
- the **external** backend provisions nothing, so the join walk is skipped rather than failed

A result is only as good as the backend that produced it. Before reporting a suite run, check which steps **skipped** rather than only which failed - a skip on this suite usually means a capability the backend does not have, and reading a run with fifteen of them as a pass is the mistake this section exists to prevent. [`test/AGENTS.md`](test/AGENTS.md) has the per-backend detail and the gaps each one leaves.

None of them need elevation, and neither does inspecting one. The suite brings its fleet up and tears it down itself, and a whole run logs no `sudo`, no polkit and no `modprobe` - so read the run's own log to tell whether it is healthy. In particular **do not reach for `virsh`**: `virsh -c qemu:///system` goes through polkit and interrupts whoever is at the keyboard with an authentication prompt, to answer a question the log already answers. A fleet a killed run left behind comes down with `mage dev:vmDown -fleet=<name>`, which needs no prompt either.

### Measuring binary size means building the way a release does

Checking the budget above means producing a number that means something, which is easy to get wrong, because a plain `go build` of this repo is about 35% larger than the shipped artifact and the gap is not uniform: `CGO_ENABLED=0` alone accounts for most of it, and `-gcflags=all="-l"` for another 13%.

Read [`docs/dev/build-flags.md`](docs/dev/build-flags.md) before quoting a number. It documents every flag, why it is set, and what each one measured. The flags themselves live in `pkg/utils/build/utils.go` (`LDFlags`, `GCFLags`), which `magefiles/pkg/build/build.go` calls - so the one safe way to build a comparable binary is `mage build:binary`, and the one safe way to spell it by hand is to read those two functions rather than to remember them.

```sh
mage build:binary                        # the shipped flags, no remembering required
```

A size measured with different flags than the baseline it is compared against is worse than no measurement, because it looks like a result. Two further traps, both of which have produced wrong numbers in this repo:

- **Compare like with like, across the whole set.** Rebuild the baseline with the same flags in the same session rather than reusing a figure from a document or an earlier run. `docs/dev/build-flags.md` records a flag comparison at one point in history, not a size to diff against today; toolchain and dependency drift move the absolute number without anybody editing the file.
- **Vendor size is not binary size, and neither is module count.** Dropping a module from `go.mod` shrinks `vendor/` and the dependency surface whether or not the linker was keeping any of it. Removing `containerd/containerd/v2` deleted 5065 vendored lines and moved the binary by 30KB. Both facts are worth reporting; reporting the first as though it were the second is not. Use `go list -deps ./cmd/cargoship` to see what is actually linked, and attribute weight with `go mod why -vendor <pkg>`.

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
git check-attr linguist-generated -- docs/ansible/cargoship/module_apply.md
```

## Keeping `README.md` current

[`README.md`](README.md) is the repository's front page and the pitch: what cargoship does, which distros it supports, how to install it, and one example per phase. It is also the source for the book's landing page - `magefiles/pkg/gen/docs/book.go` copies it to `docs/index.md` - so a stale README is stale in two places.

Treat it as part of the change, not a follow-up:

- **a feature lands** - add it where a reader would look for it. A new phase or command belongs in `Usage Examples` with a runnable snippet; a new distro in `Supported Distributions`; a new install channel in `Installation`; a new config file or schema in `Configuration and Schemas`.
- **a feature is removed or renamed** - delete or rename every mention, including the example that exercised it. A documented flag that no longer exists is worse than no documentation.
- **behaviour changes under an existing heading** - fix the prose and the snippet together. Commands shown in `README.md` are copied and pasted; they should work as written against the current build.

The generated command and phase reference under `docs/` comes from the code, so it corrects itself on `go run ./magefiles/core generate:document`. `README.md` does not - it is hand-written, and nothing fails if it drifts. After editing it, run that target so `docs/index.md` matches.

## Say so when you notice user-facing drift, even out of scope

Documentation and user-facing strings drift silently. Nothing fails, no test goes red, and the generated pages under `docs/` regenerate the stale text faithfully - so a string that describes a capability the code removed can survive several releases, looking authoritative the whole time. The `--signing-key` flag help advertised `awskms://`, `gcpkms://`, `azurekms://` and `hashivault://` for months after `pkg/signing` had been rewritten to reject all four by name, because the wording lived in a cobra flag string and a `jsonschema` tag rather than in prose anybody reread.

So drift noticed is drift reported. When work in one corner of the tree turns up a stale user-facing surface somewhere else - a flag description, a `Long` or example string in `config/lang/`, a `jsonschema` example in `types/config.go`, an Ansible module doc block, a README snippet, a key missing from `cargoship-config.yaml` - name it in the change description, with the file and what is wrong. Do not quietly widen the change to fix it, and do not stay silent because it is out of scope: the operator deciding whether it is worth a follow-up is the point, and an unfixed drift they know about is a fine state to leave the tree in.

Two things make this worth a rule rather than a habit:

- **the stale copy is usually upstream of several generated files**, so what looks like one wrong line in `docs/` is one wrong line in a Go string plus four regenerated pages, and a reader who corrects the page by hand loses the fix on the next `generate:document`. Chase the wording back to the string that produced it before reporting where it lives.
- **a vendored string can be the source.** Several flag descriptions come from `vendor/github.com/zarf-dev/zarf/src/config/lang`, which describes zarf's capabilities rather than cargoship's. Where cargoship's behaviour has diverged, the fix is a constant in `config/lang/` and nothing else - see the `--signing-key` strings for the pattern.

## A new flag belongs in the config file too

A flag on a cobra command is one half of how cargoship is configured. The other half is [`types/config.go`](types/config.go), the struct `v.Unmarshal(&resolvedConfig)` decodes the config file, the environment and the viper defaults into - and `resolvedConfig` is what a flag registration reads for its default value. A flag wired only into cobra works, which is why it is easy to stop there, and it leaves an operator restating on every invocation something their whole fleet has one answer to.

So adding a flag that carries a setting rather than a one-off instruction means all of:

- **a field in the right section of `types/config.go`**, with the `json` and `mapstructure` tags that name its key. The doc comment is the generated schema's description, so write it for an operator authoring YAML, and say which commands honour it when not all of them do.
- **a `v.SetDefault(configPath(...), ...)` in [`cmd/viper.go`](cmd/viper.go)**, even when the default is the Go zero value. Read the comment above the zero-value block before deciding to skip one: `Unmarshal` builds its result from `v.AllKeys()`, so a key with no default registered is invisible to it and silently loses environment-variable support.
- **the flag's default read from `resolvedConfig`**, not a literal - `cmd.Flags().StringVar(&o.thing, InstallThing, resolvedConfig.DistroOpts.Thing, lang.CmdInstallFlagThing)`. This is the line that makes the config file take effect, and the ordering falls out of it for free: flag beats config beats default.
- **an entry in `expandHomePaths`, for a path-valued field.** A shell expands a leading `~` in a flag value before cargoship sees it; nothing expands one in a config file or an environment variable, so `~/.cache/cargoship/...` reaches `os.Open` as a literal and fails naming a path a shell would have found. Add the field to the slice in `expandHomePaths` and a case to `TestExpandHomePaths`, which compares the whole struct and so fails until the expectation is written out.
- **a line in [`cargoship-config.yaml`](cargoship-config.yaml)**, the sample config at the repository root. It is what an operator copies and what the `README.md` examples all point `--config` at, so a key missing from it is a key nobody discovers. Add it commented out, with its one-line explanation, where the setting should not be on by default - a sample that writes files or changes host state on a first run is worse than one that is incomplete. `test/e2e/cargoship-config.yaml` is a fixture rather than a sample: change it only when a suite needs the key.
- **a `README.md` snippet showing the config-file form**, per the section above, and a regenerated schema - `go run ./magefiles/core generate:schema`.

Not every flag qualifies. `--confirm` and `--dry-run` are decisions about one invocation, and a config file that answered them standingly would be a hazard rather than a convenience; they stay cobra-only. The test is whether an operator would give the same answer every time they run the command.

**Where the key goes is a question for the person, not a guess.** The section a field lands in is the key an operator types, it ships in a generated schema, and moving it later breaks every config file that set it - while from inside the code two or three placements usually look equally defensible. Say which one you think is right and why, show the YAML it produces, and ask before writing it.

## Pinned versions, digests, and checksums

Pins live all over the tree - container image digests in `containers/*/Dockerfile`, `dnf` package versions, upstream distro tags in `thirdparty-src/pins.json`, cloud image checksums in Go constants - but the machinery that moves them is mage and Dependabot. Every pin needs one of those owning it, or a written bump procedure where neither fits. Before adding a pinned value anywhere, read the rule and the worked examples in [`magefiles/AGENTS.md`](magefiles/AGENTS.md#every-pin-needs-an-owner).

## Committing changes

Run the hooks over the whole tree before every commit and fix anything they flag. Prefer [`prek`](https://github.com/j178/prek) - `prek run --all-files` - if it is on `PATH`; it reads the same [`.pre-commit-config.yaml`](.pre-commit-config.yaml) and runs the same hooks, just faster. Fall back to `pre-commit run --all-files` when it isn't. Several of the hooks rewrite files (`end-of-file-fixer`, `trailing-whitespace`, `keep-sorted`, `addlicense`, doc/schema generators) - re-stage after the run. Don't skip hooks with `--no-verify` or `SKIP=`.

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

When a decision looks reversible from the code alone - four generated `osv-scanner.toml` files inside `vendor/`, a firewall backend picked by the node's OS rather than by what is running on it, an in-memory OCI registry in the test suite - check `docs/agent/` first. These are usually the record of a tradeoff already made on purpose, not an oversight waiting to be cleaned up.

# Markdown line formatting

## Do not hard-wrap lines in Markdown documents

Markdown files should NOT have hard-wrapped or truncated lines. Instead, write full paragraphs and list items on single, continuous lines without manual line breaks, letting editors and renderers handle line wrapping dynamically.

- Write entire paragraphs or list items on a single line without manual line breaks / hard wraps.
- Let editors and rendering tools handle line wrapping dynamically.

## Pad table cells so the columns line up

Tables are written in their aligned form, so that the source reads as a table and a reviewer can see the columns without rendering the file. Pad every cell with spaces to the width of the widest cell in its column, keep one space of padding inside each pipe, and run the dashes of the delimiter row out to that same width. Every line of the table then has the same length.

Write this:

```markdown
| Path                                   | What it holds     |
| -------------------------------------- | ----------------- |
| `.spec.config.registries[N].auth.user` | Registry username |
```

Not this:

```markdown
| Path | What it holds |
| --- | --- |
| `.spec.config.registries[N].auth.user` | Registry username |
```

Adding a row that is wider than the column repads the whole table, which is expected - the alignment is part of the content, not a one-time cleanup.
