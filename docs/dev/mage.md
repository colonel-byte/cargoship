# Mage Tasks and Automation

This document explains how Cargoship uses [Mage](https://magefile.org/) to orchestrate build, test, and generation tasks.

## Overview

Cargoship uses `mage` as its primary task runner and automation tool instead of a traditional `Makefile`. The implementation resides in the `magefiles/` directory, which acts as the central repository for:

*   Building binaries (both containerized with Dagger and natively on the host).
*   Running end-to-end (e2e) tests.
*   Generating documentation from the codebase.
*   Generating and publishing JSON schemas from Go types.

The entry point of the automation layer is `magefiles/core/core.go`, which bootstraps the runtime and exposes exported functions as Makefile-like executable targets.

## Namespace Architecture

Mage targets are organized into logical Go namespaces to group related operations together. Every target is invoked as `mage <namespace>:<target>`, and target names are case-insensitive, so `mage generate:document` and `mage generate:Document` are the same command. Run `mage -l` for the authoritative list on your checkout.

### `Dagger` Namespace

The `Dagger` namespace is the default target and the primary build path. It manages containerized and reproducible builds of the Cargoship binary:

*   `Toolchain` — Configures the local Dagger toolchain.
*   `Binary` — Compiles the binary for the host's platform.
*   `Linuxamd64` / `Linuxarm64` — Compiles Linux binaries.
*   `Macamd64` / `Macarm64` — Compiles macOS binaries.
*   `All` — Compiles and exports all release binaries to `build/` concurrently.

```sh
mage dagger:toolchain     # update the dagger build environment (run once, or after a dagger upgrade)
mage dagger:binary        # build for this host's OS/arch
mage dagger:linuxamd64    # build build/cargoship_linux_amd64
mage dagger:linuxarm64    # build build/cargoship_linux_arm64
mage dagger:macamd64      # build build/cargoship_darwin_amd64
mage dagger:macarm64      # build build/cargoship_darwin_arm64
mage dagger:all           # build every release binary into build/
mage                      # same as `mage dagger:all` -- it is the default target
```

### `Build` Namespace

The `Build` namespace mirrors the compilation targets of the `Dagger` namespace but bypasses containerization, executing compilation natively on the host's Go toolchain. This path is intended for quick, local development, and it is the one to use when Docker or Dagger is not available.

```sh
mage build:binary         # build for this host's OS/arch, no container
mage build:linuxamd64
mage build:linuxarm64
mage build:macamd64
mage build:macarm64
mage build:all            # build every release binary into build/
```

### `Dev` Namespace

The `Dev` namespace aggregates convenience tasks for day-to-day development:

*   `Clean` — Deletes local compilation artifacts and cleans the `build/` directory.
*   `Tidy` — Runs `go mod tidy` inside the workspace.
*   `Vendor` — Runs `Tidy`, then `go mod vendor`. Use this rather than a bare `go mod vendor` after changing dependencies, so `go.mod`, `go.sum`, and `vendor/` are updated in one step.
*   `Digest` — Resolves `docker.io/library/alpine:latest` through the host's Docker credentials and prints its digest. This is a connectivity and auth smoke test, not part of a build.

```sh
mage dev:clean            # rm the build/ artifacts
mage dev:tidy             # go mod tidy
mage dev:vendor           # go mod tidy, then go mod vendor
mage dev:digest           # print the alpine:latest digest, to check registry auth works
```

### `Test` Namespace

The `Test` namespace hosts the integration and validation suites:

*   `EndToEnd` — Builds Cargoship for the host via Dagger, then runs the full Go end-to-end suite in verbose mode. It builds first every time, so there is no separate build step to remember.

```sh
mage test:endToEnd        # build via dagger, then run the e2e suite
```

### `Generate` Namespace

The `Generate` namespace handles code-generation and repository asset updates:

*   `Document` — Automatically generates command documentation from Cobra structures, parses cluster operational phase descriptors, and formats the mdBook `docs/SUMMARY.md` structure.
*   `Schema` — Generates YAML-compatible JSON schemas in `schema/` from Go structs using reflection, facilitating IDE autocomplete and validation for cluster config, distro packages, and runtime configs.
*   `PullEngineSource` — Fetches raw k3s/RKE2 source at the tags pinned in `thirdparty-src/pins.json` into `thirdparty-src/` (see [thirdparty-src](thirdparty-src.md)). Touches the network.
*   `LatestTag <distro> <vMAJOR.MINOR>` — Resolves the newest non-RC upstream tag for that minor line, pins it in `thirdparty-src/pins.json`, and re-pulls that version's source if the pin moved. Touches the network.
*   `UpdatePins` — Runs `LatestTag` over every minor line already pinned in `thirdparty-src/pins.json`, refreshing each to its newest patch release. Touches the network.

```sh
mage generate:document                  # regenerate docs/commands, docs/phases, and docs/SUMMARY.md
mage generate:schema                    # regenerate schema/*.json from the Go API types

mage generate:pullEngineSource          # re-pull every tag already pinned in thirdparty-src/pins.json
mage generate:latestTag rke2 v1.36      # pin rke2's newest v1.36.x, and pull it if the pin moved
mage generate:latestTag k3s v1.31       # same, for a k3s minor line
mage generate:updatePins                # bump every pinned minor line, both distros, to its newest patch
```

`LatestTag` is also how a *new* minor line is added: pass a prefix that `thirdparty-src/pins.json` does not yet pin and it appends that line rather than replacing one. Adding an rke2 minor means adding the matching k3s minor too, because rke2's config is composed against k3s's flags at the same version:

```sh
mage generate:latestTag k3s v1.37
mage generate:latestTag rke2 v1.37
```

```sh
mage generate:document                  # regenerate docs/commands, docs/phases, and docs/SUMMARY.md
mage generate:schema                    # regenerate schema/*.json from the Go API types
```

---

## File-by-File Reference

*   **`core/core.go`:** Configures the bootstrap process and imports distro-specific modules to register Go side-effects before task execution. Lives in its own subpackage (rather than directly in `magefiles/`) so it does not collide with the `func main()` that the `mage` CLI generates on the fly — see [Running Mage Directly](#running-mage-directly-without-the-cli) below.
*   **`dagger.go`:** Houses user-facing targets for containerized compilation via Dagger.
*   **`build.go`:** Defines compilation tasks utilizing the local host Go toolchain.
*   **`dev.go`:** Defines convenience tasks under the `Dev` and `Test` namespaces.
*   **`gen-docs.go`:** Performs Cobra command extraction and phase parser generation to update everything inside the `docs/` tree.
*   **`gen-schema.go`:** Maps Go types to JSON schemas under `schema/`.
*   **`gen-engine-source.go`:** Holds `Generate.PullEngineSource` and the clone-and-copy logic behind it.
*   **`gen-engine-latest-tag.go`:** Holds `Generate.LatestTag`.
*   **`gen-engine-update-pins.go`:** Holds `Generate.UpdatePins`.
*   **`engine-pins.go`:** Shared, target-free layer over `thirdparty-src/pins.json`: reading, writing, tag parsing, and tag resolution used by the `gen-engine-*.go` targets.
*   **`utils.go`:** Implements low-level helper functions for file cleanup, Dagger CLI execution, and compiler flag construction. See [build-flags](build-flags.md) for what each flag/env var does and why.
*   **`binary.go`:** Includes non-exported validation functions to verify binary existences within `GOPATH`.

---

## Managed Outputs

Running various Mage tasks maintains and updates the following filesystem artifacts:

| Output Directory / File | Description | Target |
| :--- | :--- | :--- |
| `build/cargoship_*` | Compiled release binaries | `Dagger.All` / `Build.All` |
| `docs/commands/*` | Auto-generated CLI documentation | `Generate.Document` |
| `docs/phases/*` | Auto-generated cluster phase descriptors | `Generate.Document` |
| `docs/SUMMARY.md` | Compiled table of contents for mdBook | `Generate.Document` |
| `schema/*.json` | JSON schemas for YAML validations | `Generate.Schema` |
| `thirdparty-src/<distro>/<minor>/*` | Raw pinned upstream k3s/RKE2 source | `Generate.PullEngineSource` / `Generate.LatestTag` |
| `thirdparty-src/pins.json` | Pinned upstream tags | `Generate.LatestTag` / `Generate.UpdatePins` |

---

## Running Mage Directly (Without the CLI)

Normally you invoke tasks through the installed `mage` binary, e.g. `mage dagger:binary`. The `mage` CLI works by scanning `magefiles/` for exported functions and namespaces, then generating its own `func main()` (written to a gitignored `mage_output_file.go`) that wires those functions up to CLI subcommands before compiling and running the result.

Because that generated file declares `package main` with its own `func main()`, it cannot coexist with a second, hand-written `func main()` in the same package — hence `core/core.go` (which does exactly that, via `mage.Main()`) is split out into its own `magefiles/core` subpackage rather than sitting alongside the task files in `magefiles/`.

This split means `core/core.go` can also be run on its own, bypassing the `mage` CLI entirely:

```sh
go run ./magefiles/core
```

This builds and runs the same `mage.Main()` entry point that the `mage` CLI would otherwise generate for you. It's useful when:

*   The `mage` binary isn't installed on the host (e.g. a minimal CI or container image that already has a Go toolchain).
*   You want a single, explicit `go run` invocation instead of depending on a separately-installed tool.

Task selection still works the same way — pass the namespace:target as an argument, e.g.:

```sh
go run ./magefiles/core dagger:binary
```

Note that `magefiles/` itself remains its own `package main` for the `mage` CLI's benefit; `core/core.go` is a separate package and binary, not part of that compiled unit.
