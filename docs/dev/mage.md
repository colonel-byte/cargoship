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

Mage targets are organized into logical Go namespaces to group related operations together. Every target is invoked as `mage <namespace>:<target>`, and target names are case-insensitive, so `mage generate:engineconfig` and `mage generate:engineConfig` are the same command. Run `mage -l` for the authoritative list on your checkout.

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
*   `Examples` — Renders `magefiles/templates/<distro>-distro.yaml.tmpl` into `example/<distro>-<cni>/<minor>/v<version>/distro.yaml`, one flavor per CNI (`example/rke2-cilium/`, `example/rke2-canal/`, `example/k3s-flannel/`), grouped by minor line (`v1_35/`, matching `thirdparty-src/<distro>/` and `src/pkg/engineconfig/gen/<distro>/`) and creating those directories as needed. Per flavor it renders once per tag of that distro pinned in `thirdparty-src/pins.json` and once per example directory that flavor already has on disk, so a template edit reaches the older examples instead of leaving them to drift. Everything that varies between versions is derived from the tag, except `imageConfig.images` and the digests, which come from that release's published assets. Touches the network.

    Distros and their flavors are declared together in `exampleDistros` in `magefiles/examples.go`. Flavors differ by more than their image list: cilium replaces kube-proxy (`disable-kube-proxy: true`) and is configured through an `rke2-cilium` HelmChartConfig manifest, while canal and flannel run alongside kube-proxy and carry no manifest of their own. Adding a CNI means adding a flavor entry there, and a `{{ if }}` in that distro's template for anything specific to it.

    What differs between the distros themselves sits in three hooks on the distro entry, so the rendering itself stays single-copy: `derive` builds the URLs only that distro has, `probe` names the URL whose absence retires a build, and `fetch` pulls anything else only the release can answer. RKE2 installs versioned `rke2-common`/`server`/`agent` RPMs from `rpm.rancher.io` and splits its images across a core manifest plus one per CNI. k3s installs a single binary straight from its GitHub release, ships flannel inside that binary — so `k3s-images.txt` is the whole list and there is nothing per-CNI to fetch — and publishes `sha256sum-amd64.txt`, which is where the binary's `shasum` comes from rather than downloading 70 MB per version to hash it. Both distros install their distro's selinux policy RPM.

    k3s examples share one copy of the unit files and killall script, kept in `example/k3s/core/` and referenced as `../../../k3s/core/…`; relative `source` paths resolve against the directory holding `distro.yaml`.

    Each RPM entry also carries a `shasum`, so cargoship can verify the file it downloads. Hashing an RPM means downloading it, and `rke2-common` alone is around 29 MB, so the digests are cached in `example/shasums.json`. It is a committed map keyed by RPM file name, each entry holding the `url` it was fetched from and its `sha256`, so the file reads as an inventory of what the examples install. Only files missing from it are fetched, which makes a re-render free and a new release cost just its own four RPMs. An entry whose `url` no longer matches the one being rendered is re-hashed rather than trusted, so moving an RPM cannot hand back a stale digest. Delete an entry to force it to be re-hashed.

    Before rendering, each build's `rke2-common` RPM is checked (a `HEAD`, and only for URLs the cache has never seen). Rancher supersedes an `rke2rN` with the next revision and removes the old RPMs, while the git tag and image manifests stay up — so a build can look renderable and still install nothing. Those are skipped, and any example already on disk for one is deleted, along with its minor line directory if that empties it. As of August 2026 that covers `v1.35.0+rke2r2` and `v1.35.3+rke2r2`, replaced by `rke2r3`, and `v1.34.3+rke2r2` and `v1.34.6+rke2r2` — use the `rke2r3` release of those patches. Nothing is remembered about a skip, so a build that comes back is rendered again on the next run; a build that goes away is only noticed while it is still pinned or still on disk. The `shasum` on an individual file is still allowed to be missing — that path now only covers the odd file a still-published build lost.

*   `ExampleLine <distro> <vMAJOR.MINOR>` — Renders an example for *every* non-RC release on one minor line of that distro, rather than only the pinned one, so `rke2 v1.36` backfills `v1.36.0+rke2r1` through the newest `v1.36` release into `example/rke2-cilium/v1_36/` and `example/rke2-canal/v1_36/`. The leading `v` is optional. Once written, `Examples` keeps those files current, since it re-renders every example directory on disk. Touches the network.
*   `EngineConfig` — Statically parses raw engine source under `thirdparty-src/` (see [thirdparty-src](thirdparty-src.md)) to generate typed `config.yaml` structs per distro/version in `src/pkg/engineconfig/gen/`.

```sh
mage generate:document                  # regenerate docs/commands, docs/phases, and docs/SUMMARY.md
mage generate:schema                    # regenerate schema/*.json from the Go API types

mage generate:pullEngineSource          # re-pull every tag already pinned in thirdparty-src/pins.json
mage generate:latestTag rke2 v1.36      # pin rke2's newest v1.36.x, and pull it if the pin moved
mage generate:latestTag k3s v1.31       # same, for a k3s minor line
mage generate:updatePins                # bump every pinned minor line, both distros, to its newest patch
mage generate:engineConfig              # regenerate src/pkg/engineconfig/gen/ from what is on disk
mage generate:examples                  # re-render every example/<distro>-<cni>/<minor>/*/distro.yaml
mage generate:exampleLine rke2 v1.36    # render an example for every rke2 release on the 1.36 line
mage generate:exampleLine k3s 1.36      # same for k3s -- the leading v is optional
```

`LatestTag` is also how a *new* minor line is added: pass a prefix that `thirdparty-src/pins.json` does not yet pin and it appends that line rather than replacing one. Adding an rke2 minor means adding the matching k3s minor too, because rke2's config is composed against k3s's flags at the same version:

```sh
mage generate:latestTag k3s v1.37
mage generate:latestTag rke2 v1.37
mage generate:engineConfig
mage generate:examples
```

The usual order after any pin change is `updatePins` (or `latestTag`), then `engineConfig`, then `examples` — the first two write what the next one reads, and only the pin-moving targets touch the network for source. `engineConfig` is fully offline.

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
*   **`gen-engine-config.go`:** Holds `Generate.EngineConfig`, including RKE2's flag composition against k3s.
*   **`gen-examples.go`:** Holds `Generate.Examples`.
*   **`gen-example-line.go`:** Holds `Generate.ExampleLine`.
*   **`examples.go`:** Shared, target-free layer behind both example targets: what an example is rendered from (the tag-derived fields and the fetched image manifests) and how one is written.
*   **`example-shasums.go`:** The `example/shasums.json` cache, and the `sha256` function the example template hashes its remote files with.
*   **`engine-pins.go`:** Shared, target-free layer over `thirdparty-src/pins.json`: reading, writing, tag parsing, and tag resolution used by the four `gen-engine-*.go` targets.
*   **`templates/`:** Text templates the generation targets render: `rke2-distro.yaml.tmpl` and `k3s-distro.yaml.tmpl`, one per distro that has examples.
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
| `src/pkg/engineconfig/gen/*` | Typed engine `config.yaml` structs per distro/version | `Generate.EngineConfig` |
| `thirdparty-src/<distro>/<minor>/*` | Raw pinned upstream k3s/RKE2 source | `Generate.PullEngineSource` / `Generate.LatestTag` |
| `thirdparty-src/pins.json` | Pinned upstream tags | `Generate.LatestTag` / `Generate.UpdatePins` |
| `example/<distro>-<cni>/<minor>/*/distro.yaml` | Rendered rke2 and k3s example packages, one directory per CNI flavor, grouped by minor line | `Generate.Examples` |
| `example/shasums.json` | Cached sha256 of every remote file the examples hash | `Generate.Examples` / `Generate.ExampleLine` |

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
