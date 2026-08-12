# Mage Tasks and Automation

This document explains how Cargoship uses [Mage](https://magefile.org/) to orchestrate build, test, and generation tasks.

## Overview

Cargoship uses `mage` as its primary task runner and automation tool instead of a traditional `Makefile`. The implementation resides in the `magefiles/` directory, which acts as the central repository for:

*   Building binaries (both containerized with Dagger and natively on the host).
*   Running end-to-end (e2e) tests.
*   Generating documentation from the codebase.
*   Generating and publishing JSON schemas from Go types.

The entry point of the automation layer is `magefiles/main.go`, which bootstraps the runtime and exposes exported functions as Makefile-like executable targets.

## Namespace Architecture

Mage targets are organized into logical Go namespaces to group related operations together.

### `Dagger` Namespace

The `Dagger` namespace is the default target and the primary build path. It manages containerized and reproducible builds of the Cargoship binary:

*   `Toolchain` — Configures the local Dagger toolchain.
*   `Binary` — Compiles the binary for the host's platform.
*   `Linuxamd64` / `Linuxarm64` — Compiles Linux binaries.
*   `Macamd64` / `Macarm64` — Compiles macOS binaries.
*   `All` — Compiles and exports all release binaries to `build/` concurrently.

Running `mage` without arguments defaults to `Dagger.All`.

### `Build` Namespace

The `Build` namespace mirrors the compilation targets of the `Dagger` namespace but bypasses containerization, executing compilation natively on the host's Go toolchain. This path is intended for quick, local development.

### `Dev` Namespace

The `Dev` namespace aggregates convenience tasks for day-to-day development:

*   `Clean` — Deletes local compilation artifacts and cleans the `build/` directory.
*   `Tidy` — Runs `go mod tidy` inside the workspace.
*   `ResolveImageDigest` — Resolves and logs specific container digests for validation.

### `Test` Namespace

The `Test` namespace hosts the integration and validation suites:

*   `E2E` — Automatically compiles Cargoship via Dagger and executes the complete Go-based end-to-end test suite in verbose mode.

### `Generate` Namespace

The `Generate` namespace handles code-generation and repository asset updates:

*   `Document` — Automatically generates command documentation from Cobra structures, parses cluster operational phase descriptors, and formats the mdBook `docs/SUMMARY.md` structure.
*   `Schema` — Generates YAML-compatible JSON schemas in `schema/` from Go structs using reflection, facilitating IDE autocomplete and validation for cluster config, distro packages, and runtime configs.

---

## File-by-File Reference

*   **`main.go`:** Configures the bootstrap process and imports distro-specific modules to register Go side-effects before task execution.
*   **`dagger.go`:** Houses user-facing targets for containerized compilation via Dagger.
*   **`build.go`:** Defines compilation tasks utilizing the local host Go toolchain.
*   **`dev.go`:** Defines convenience tasks under the `Dev` and `Test` namespaces.
*   **`gen-docs.go`:** Performs Cobra command extraction and phase parser generation to update everything inside the `docs/` tree.
*   **`gen-schema.go`:** Maps Go types to JSON schemas under `schema/`.
*   **`utils.go`:** Implements low-level helper functions for file cleanup, Dagger CLI execution, and compiler flag construction.
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
