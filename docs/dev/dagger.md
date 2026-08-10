# Dagger

## Overview

This repository uses [Dagger](https://dagger.io/) as its containerized build pipeline.

In practical terms, the Dagger setup is responsible for building `cargoship` binaries in a reproducible environment instead of relying entirely on the host machine's local Go toolchain.

The Dagger module for this repository is defined by `dagger.json` at the repository root and implemented in the `.dagger/` directory.

## Dagger module configuration

The root `dagger.json` file tells Dagger four important things:

- the module name is `cargoship`
- the module uses the Go SDK
- the module source code lives in `.dagger`

That means commands like `dagger call build` are resolved against the Go code in `.dagger/`.

## The `.dagger/` directory

The `.dagger/` directory is a standalone Go-based Dagger module.

Important files include:

- `main.go` — defines the core `Cargoship` Dagger object and initialization logic
- `build.go` — defines the multi-platform `Build` pipeline
- `build-local.go` — defines the single-target `BuildLocal` pipeline
- `utils/utils.go` — shared linker and compiler flags used by Dagger builds
- `dagger.gen.go` and `internal/dagger/` — generated Dagger SDK code

The module has its own `go.mod` because Dagger modules are implemented as Go programs that Dagger executes.

## Core Dagger object

The central type is `Cargoship` in `.dagger/main.go`.

It stores:

- `Source` — the mounted source tree
- `AppVersion` — the application version derived from Git tags
- `GoVersion` — the Go version parsed from the repository `go.mod`
- `IsInitialized` — whether initialization has already happened

### Initialization flow

Before a build runs, the module initializes itself by:

1. starting an `alpine` container
2. installing `git`
3. mounting the repository source into `/src`
4. running `git describe --tags --abbrev=0`
5. reading `go.mod`
6. extracting the Go version with a regex

This gives the build pipeline two key pieces of metadata:

- the release version from Git tags
- the Go toolchain version to use for the build container

The version string has the leading `v` trimmed before being stored in `AppVersion`.

## `BuildLocal`

`BuildLocal` in `.dagger/build-local.go` is the single-platform build function.

It accepts:

- `os`
- `arch`
- `source`

and returns a single built file.

### What it does

`BuildLocal`:

1. ensures the module is initialized
2. chooses a binary name like `cargoship_linux_amd64`
3. starts a build container from `golang:<detected-go-version>`
4. mounts a persistent Dagger cache at `/go/build-cache`
5. mounts the repository source at `/src`
6. sets `GOOS` and `GOARCH`
7. gets the current short Git commit with `git rev-parse --short HEAD --always`
8. computes build flags
9. runs `go build -mod=vendor ... /src/main.go`
10. returns the built file from `/bin/<name>`

### Build flags

The shared helpers in `.dagger/utils/utils.go` inject:

- `-s -w` linker flags to reduce binary size
- `-X github.com/colonel-byte/cargoship/src/config.CLIVersion=...`
- `-X github.com/colonel-byte/cargoship/src/config.CLICommit=...`

So the Dagger build embeds both the application version and the Git commit into the binary.

## `Build`

`Build` in `.dagger/build.go` is the multi-platform pipeline.

It:

- initializes the module if needed
- starts from the repository `build/` directory object
- defines target platforms:
  - `linux/amd64`
  - `linux/arm64`
  - `darwin/amd64`
  - `darwin/arm64`
  - `windows/amd64`
  - `windows/arm64`
- runs builds concurrently using `sourcegraph/conc/pool`
- adds each built file into the output directory
- returns a `dagger.Directory` containing all built artifacts

Windows binaries get the `.exe` suffix.

In other words:

- `BuildLocal` returns one file
- `Build` returns a directory of all platform builds

## How Mage uses Dagger

The repository's Mage automation treats Dagger as the primary build path.

The `magefiles/dagger.go` file exposes a `Dagger` namespace with targets such as:

- `Dagger.Toolchain`
- `Dagger.Binary`
- `Dagger.Linuxamd64`
- `Dagger.Linuxarm64`
- `Dagger.Macamd64`
- `Dagger.Macarm64`
- `Dagger.All`

### Important detail

The default Mage target is `Dagger.All`.

That means running plain `mage` builds all binaries through the Dagger pipeline.

### Mapping from Mage to Dagger calls

Mage shells out to the Dagger CLI.

#### Full build

`Dagger.All` runs:

```sh
dagger call --progress=tty --interactive=false build export --path=build
```

This executes the Dagger `Build` function and exports the returned directory into the local `build/` folder.

#### Single-platform build

The helper `daggerBuildLocal()` runs:

```sh
dagger call --progress=tty --interactive=false build-local --os=<os> --arch=<arch> export --path=build/cargoship_<os>_<arch>
```

This executes `BuildLocal` and exports the returned file to a specific path.

## How CI uses Dagger

GitHub Actions has a dedicated workflow in `.github/workflows/dagger.yaml`.

That workflow:

1. checks out the repository with full history
2. reads the Dagger engine version from `dagger.json`
3. invokes `dagger/dagger-for-github`
4. runs the `build` Dagger verb

The full Git history matters because the Dagger module derives `AppVersion` from Git tags.

The workflow also passes `DAGGER_CLOUD_TOKEN`, which suggests builds can be connected to Dagger Cloud for observability or remote execution features.

## Relationship to host builds

This repository supports two build styles:

### Dagger build path

- containerized
- version-aware
- embeds Git version/commit metadata
- default path used by Mage
- multi-platform by design

### Host build path

The non-Dagger path in `magefiles/build.go` and `magefiles/utils.go` runs `go build` directly on the host.

That path is useful for simpler local builds, but the Dagger path appears to be the preferred and more reproducible workflow.

## What Dagger is not doing here

In this repository, Dagger is focused on binary builds.

It is not currently the tool used for:

- generating docs
- generating schemas
- publishing releases
- building the mdBook site

Those responsibilities are handled elsewhere by Mage, mdBook, GitHub Actions, and GoReleaser.

## Practical summary

If you reduce the setup to the essentials, the Dagger configuration in this repo does the following:

- defines a Go-based Dagger module in `.dagger/`
- detects the app version from Git tags
- detects the Go version from `go.mod`
- builds `cargoship` in a containerized Go environment
- supports both single-platform and multi-platform outputs
- is used by both Mage and GitHub Actions
- serves as the default build pipeline for the project
