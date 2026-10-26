# Working in `magefiles/`

This directory houses the repository's task automation layer using Mage.

## Layout and Organization

The top level of `magefiles/` is strictly for Mage namespace declarations, targets, and flag parsers in `package main`. Do not place heavy domain logic, parsers, HTTP scrapers, code generators, or template walkers directly in `package main` files at the root of `magefiles/`.

All implementation logic belongs in modular Go packages under `magefiles/pkg/`:

| Path                        | Purpose                                                                                               |
| --------------------------- | ----------------------------------------------------------------------------------------------------- |
| `magefiles/build.go`        | Mage entrypoint for the `Build` namespace (`build:binary`, `build:all`, `build:examples`)             |
| `magefiles/dev.go`          | Mage entrypoint for the `Dev` namespace (`dev:clean`, `dev:tidy`, `dev:vendor`, `dev:dnfPins`, etc.)  |
| `magefiles/generate.go`     | Mage entrypoint for the `Generate` namespace (`generate:document`, `generate:schema`, etc.)           |
| `magefiles/release.go`      | Mage entrypoint for the `Release` namespace (`release:tofuProvider`, `release:tofuProviderLayout`)    |
| `magefiles/test.go`         | Mage entrypoint for the `Test` namespace (`test:endToEnd`, `test:fuzz`, etc.)                         |
| `magefiles/core/core.go`    | Bootstrapping main package for `go run ./magefiles/core`                                              |
| `magefiles/pkg/build/`      | Binary building, compiler/linker flag assembly, and example package compilation                       |
| `magefiles/pkg/devtools/`   | Developer tooling: AlmaLinux repodata parsing (`dnfpins`), Scorecard vendor overrides (`osv`)         |
| `magefiles/pkg/gen/`        | Generators: `completion/`, `docs/`, `engineconfig/` (k3s/rke2 codegen & pins), `examples/`, `schema/` |
| `magefiles/pkg/release/`    | Release artifacts goreleaser does not build: the provider's OCI mirror artifact                       |
| `magefiles/pkg/testrunner/` | End-to-end and fuzz test runners                                                                      |
| `magefiles/pkg/util/`       | Shared helper functions (aligned table formatting, git commit resolution, clean utilities)            |

## Target Structure Rules

1. **Keep Root Namespace Targets Thin**: Root Mage methods (`build.go`, `dev.go`, `generate.go`, `release.go`, `test.go`) should only perform argument defaults, environmental setup, or high-level status output, delegating immediately to the appropriate package under `magefiles/pkg/`.
2. **Encapsulate and Test**: Logic under `magefiles/pkg/` should be written as standard Go packages with unit test files (`*_test.go`) adhering to the table-driven test formatting guidelines in the root `AGENTS.md`.

## Every pin needs an owner

A pinned value is a version, tag, digest, or checksum written into the tree that names something living upstream: a container image digest, a `dnf` package version, an upstream distro tag, a cloud image's `sha256`. Pinning is the right default here - the management node is air-gapped and a build has to be reproducible - but a pin with nothing watching it goes stale silently. Nothing fails; the tree just keeps installing a version that stopped being current, and often stopped being patched, months ago.

So a new pin is not done when it is written. It is done when something can move it. Pick one of two owners and wire it up in the same change:

- **A mage target that rewrites the pin in place** - when the current value has to be *discovered* (queried from repodata, resolved from a registry, read from an upstream checksum file) and then written back into one or more files. The target does the query, the rewrite, and the reporting; the human runs it and reviews the diff. `mage dev:dnfPins` is the model: it queries AlmaLinux 10 repodata and rewrites the `ansible-core` and `bash-completion` pins across `containers/ubi/Dockerfile`, `containers/ansible/Dockerfile`, and `.goreleaser.yaml`. `mage generate:updatePins` does the same for the upstream k3s/RKE2 tags in `thirdparty-src/pins.json`.
- **A [`.github/dependabot.yaml`](../.github/dependabot.yaml) entry** - when the pin sits in a file format Dependabot already parses, so it costs an ecosystem entry rather than a generator. The `docker` entries keep the digests in `containers/*/Dockerfile` current; `gomod`, `github-actions`, `pre-commit`, and `npm` cover the rest. A new `containers/<name>/Dockerfile` needs its own entry - one per directory, ungrouped, for the commitlint reason noted in that file.

Prefer the Dependabot entry when the format is already supported: it is a few lines, it opens the PR unprompted, and it needs nobody to remember. Reach for a mage target when the value lives somewhere no updater reads - a Go constant, a bespoke JSON manifest, a checksum beside a URL.

The rule holds for values pinned outside `magefiles/` too, which is most of them. This section lives here because the automation does.

If neither fits, say so where the pin is. The microvm guest images in `internal/microvm/image.go` carry a `url` and a `sha256` that nothing updates automatically - bumping them is a written procedure in [`docs/dev/microvm.md`](../docs/dev/microvm.md) instead, because the new build's date-stamped path and checksum come from a directory listing no ecosystem parses. A documented manual bump is an acceptable third answer. An undocumented one is not: a reader who finds a bare hash and no way to move it has no way to tell a deliberate pin from an abandoned one.

When adding automation, keep the two halves honest with each other. A mage target that rewrites a pin must be run and its output committed in the same change, or the first person to run it gets a diff they did not write. A Dependabot entry that cannot authenticate to its registry needs the registry block as well - see the `docker-hub` entry.
