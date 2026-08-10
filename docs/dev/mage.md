# Magefiles

> Mage is a make/rake-like build tool using Go. You write plain-old go functions, and Mage automatically uses them as Makefile-like runnable targets.

From `magefile.org/`

## About

This repository uses `mage` as its task runner instead of a traditional `Makefile`. In practice, the `magefiles/` directory is the project's automation layer for:

- building binaries
- running developer utility tasks
- running end-to-end tests
- generating documentation
- generating JSON schemas

The Mage entrypoint is `magefiles/main.go`, which calls `mage.Main()` and lets Mage discover exported targets from the other Go files in that directory.

## How the magefiles are organized

The code in `magefiles/` is grouped into namespaces. Each namespace becomes a family of Mage targets.

### `Dagger`

The `Dagger` namespace is the main build pipeline and is also the default Mage target.

It provides targets to:

- update the Dagger toolchain
- build a binary for the current platform
- build Linux amd64 and arm64 binaries
- build macOS amd64 and arm64 binaries
- build all binaries into `build/`

This is the containerized or reproducible build path and appears to be the preferred build workflow for the project.

### `Build`

The `Build` namespace provides host-based builds without Dagger.

It mirrors the Dagger targets and can:

- build for the current platform
- cross-build Linux amd64 and arm64
- cross-build macOS amd64 and arm64
- build all of them sequentially

This is the simpler local Go build path.

### `Dev`

The `Dev` namespace contains developer convenience tasks.

It currently provides targets to:

- clean local build artifacts
- run `go mod tidy`
- resolve and print a container image digest for testing

These tasks are more for day-to-day development than for release builds.

### `Test`

The `Test` namespace currently contains the end-to-end test target.

It:

- builds a local binary using Dagger
- runs the Go end-to-end test suite with verbose output

### `Generate`

The `Generate` namespace is used for generated project assets.

It provides targets to:

- generate documentation
- generate JSON schemas

This is where most code-generation style maintenance lives.

## File-by-file overview

### `main.go`

This is the Mage bootstrap file. It calls `mage.Main()` so Mage can discover and run targets from the rest of the directory.

It also imports distro configuration code for side effects so tasks that depend on those registrations can run correctly.

### `dagger.go`

This file defines the `Dagger` namespace.

It contains targets for:

- `Toolchain`
- `Binary`
- `Linuxamd64`
- `Linuxarm64`
- `Macamd64`
- `Macarm64`
- `All`

It also sets the default Mage target to `Dagger.All`, so running plain `mage` should build all binaries through Dagger.

### `build.go`

This file defines the `Build` namespace.

It offers the same platform-oriented build targets as `Dagger`, but uses the host Go toolchain directly instead of the Dagger pipeline.

### `dev.go`

This file defines two namespaces:

- `Dev`
- `Test`

`Dev` contains maintenance and debugging helpers. `Test` contains the end-to-end test target.

### `gen-docs.go`

This file defines part of the `Generate` namespace and is responsible for documentation generation.

The `Generate.Document` target:

- recreates `docs/commands`
- recreates `docs/phases`
- generates CLI command documentation from Cobra commands
- generates phase documentation from internal action definitions
- rebuilds `docs/SUMMARY.md`

In other words, it keeps generated docs synchronized with the current code.

### `gen-schema.go`

This file defines the schema-generation part of the `Generate` namespace.

The `Generate.Schema` target generates JSON schema files for several project configuration types, including cluster, distro package, and distro config definitions.

It uses Go type reflection and then applies a small amount of post-processing so the resulting schemas better match the project's YAML needs.

### `utils.go`

This file contains shared helper functions used by other magefiles.

These helpers:

- build binaries with Dagger
- build binaries with the local Go toolchain
- remove existing build artifacts

Most of the actual build mechanics live here, while `dagger.go` and `build.go` mainly expose user-facing targets.

### `binary.go`

This file contains small helper functions for working with binaries in `GOPATH/bin`.

Right now it does not expose user-facing Mage targets, but it provides reusable helper logic for checking whether a binary exists and for constructing its path.

## Generated outputs

The Mage targets in this directory are responsible for maintaining several kinds of generated output, including:

- `build/cargoship_*`
- `docs/commands/*`
- `docs/phases/*`
- `docs/SUMMARY.md`
- `schema/*.json`

## Practical summary

If you think of `magefiles/` as the project's automation layer, the responsibilities break down like this:

- use `Dagger` for the primary build workflow
- use `Build` for local non-Dagger builds
- use `Dev` for cleanup and maintenance tasks
- use `Test` for end-to-end testing
- use `Generate` for docs and schema generation

That makes the `magefiles/` folder the main place where build, test, and generation workflows are encoded for the repository.
