# Dagger Build Pipeline

This document describes how Cargoship uses [Dagger](https://dagger.io/) to orchestrate reproducible, containerized builds.

## Overview

Cargoship uses Dagger as its primary build pipeline to compile cross-platform binaries in a consistent environment. Containerizing the build process avoids environmental drift and reduces dependency on the host's local Go toolchain.

The Dagger module is defined by `dagger.json` at the repository root, with its implementation located in the `.dagger/` directory.

## Module Structure

The `.dagger/` directory is a standalone Dagger module written in Go:

*   **`main.go`** — Defines the core `Cargoship` Dagger object and metadata initialization logic.
*   **`build.go`** — Implements the multi-platform `Build` pipeline.
*   **`build-local.go`** — Implements the single-target `BuildLocal` compilation.
*   **`utils/utils.go`** — Provides shared build configuration, including compiler and linker flags.
*   **`dagger.gen.go` & `internal/dagger/`** — Contain the auto-generated Dagger SDK.

The module manages its own `go.mod` file since Dagger runs the module code as an isolated Go program.

## Core Module Logic

The entry point of the module is the `Cargoship` struct defined in `.dagger/main.go`. It retains state and tracks build-related metadata:

*   **`Source`** — The mounted repository source tree.
*   **`AppVersion`** — The application version, dynamically derived from Git tags.
*   **`GoVersion`** — The Go toolchain version, dynamically parsed from the root `go.mod`.

### Initialization Flow

Before executing any build, the module automatically resolves metadata by:

1.  Spinning up an `alpine` container and installing `git`.
2.  Mounting the repository source and running `git describe --tags --abbrev=0` to determine the release version.
3.  Reading the root `go.mod` to parse the required Go toolchain version using a regular expression.

This metadata ensures that the build uses the correct compiler image and embeds precise version info into the compiled binaries.

## Build Functions

The module exposes two main API entry points for compilation.

### `BuildLocal`

`BuildLocal` compiles Cargoship for a single target platform.

*   **Parameters:** `os` (string), `arch` (string), `source` (Directory)
*   **Returns:** `File` (the compiled binary)

During execution, `BuildLocal` starts a container using the official `golang` image matching the parsed Go toolchain version. It mounts a persistent Go build cache (`/go/build-cache`), sets `GOOS` and `GOARCH`, and compiles the application.

Shared utilities in `.dagger/utils/utils.go` inject optimization and metadata flags:
*   `-s -w` linker flags to strip debug symbols and reduce binary size.
*   `-X` variables to embed the `AppVersion` and current Git commit SHA into the binary configuration.

### `Build`

`Build` compiles Cargoship for all supported release platforms concurrently.

*   **Supported Platforms:**
    *   `linux/amd64`
    *   `linux/arm64`
    *   `darwin/amd64`
    *   `darwin/arm64`
    *   `windows/amd64`
    *   `windows/arm64`
*   **Returns:** `Directory` (containing all compiled binaries, with `.exe` extensions appended for Windows targets)

The multi-platform build executes concurrent compilation tasks utilizing a `sourcegraph/conc` concurrency pool. By default all 6 platform builds run at once; pass `--concurrency=<n>` to cap how many run in parallel (e.g. if the shared `go-build-<version>` cache volume becomes a bottleneck, or the engine has limited CPU):

```sh
dagger call build --concurrency=3 export --path=build
```

`mage dagger:all` passes this through via the `CARGOSHIP_DAGGER_CONCURRENCY` env var:

```sh
CARGOSHIP_DAGGER_CONCURRENCY=3 mage dagger:all
```

## Orchestration and Integrations

### Mage Integration

Mage serves as the task runner and interfaces directly with the Dagger CLI. The targets in `magefiles/dagger.go` wrap Dagger invocations:

*   `mage` or `mage dagger:all` triggers the full multi-platform build:
    ```sh
    dagger call --progress=tty --interactive=false build export --path=build
    ```
*   Single-platform Mage targets (such as `mage dagger:linuxamd64`) invoke `BuildLocal`:
    ```sh
    dagger call --progress=tty --interactive=false build-local --os=<os> --arch=<arch> export --path=build/cargoship_<os>_<arch>
    ```

### GitHub Actions CI

The Dagger pipeline is executed in CI via `.github/workflows/dagger.yaml`. The workflow:

1.  Checks out the repository with full git history (required to resolve `AppVersion` from tags).
2.  Configures the Dagger CLI and Engine.
3.  Runs `dagger call build export` to compile and cache the binaries.
4.  Optionally integrates with Dagger Cloud via `DAGGER_CLOUD_TOKEN` for execution insights and telemetry.

### Using Dagger Cloud locally

`DAGGER_CLOUD_TOKEN` isn't CI-only -- the Dagger CLI reads it from the environment wherever it runs, and `mage`'s shell-out inherits your shell's environment unchanged. Exporting it locally gets you the same distributed build cache and trace/insights view CI uses (visible at [dagger.cloud](https://dagger.cloud)), so a local `mage dagger:all` can warm/reuse cache entries that CI already populated, and vice versa:

```sh
export DAGGER_CLOUD_TOKEN=<token>
mage dagger:all
```

If you're running against a remote Dagger Engine instead of the local Docker one (e.g. a shared self-hosted engine), point the CLI at it with `_EXPERIMENTAL_DAGGER_RUNNER_HOST` (see [Dagger's custom runner docs](https://docs.dagger.io/configuration/custom-runner/)) -- same deal, no repo changes needed, it's picked up from the environment:

```sh
export _EXPERIMENTAL_DAGGER_RUNNER_HOST=tcp://engine.example.com:2345
mage dagger:all
```

## Containerized vs. Host Builds

Cargoship supports two parallel compilation workflows:

1.  **Dagger Build Path (Default):** Containerized, version-aware, hermetic, and capable of concurrent cross-compilation. This is the default path utilized by Mage and GitHub Actions.
2.  **Host Build Path:** Executes `go build` directly on the local machine using host-level Go toolchains (implemented in `magefiles/build.go`). This path is preserved for quick, un-containerized local development.

Dagger in this repository is strictly focused on binary compilation; other repository tasks like documentation generation, YAML schema updates, and GitHub releases are managed separately through Mage, mdBook, and GoReleaser.
