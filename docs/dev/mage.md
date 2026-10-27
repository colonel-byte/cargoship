# Mage Tasks and Automation

This document explains how Cargoship uses [Mage](https://magefile.org/) to orchestrate build, test, and generation tasks.

## Overview

Cargoship uses `mage` as its primary task runner and automation tool instead of a traditional `Makefile`. The implementation resides in the `magefiles/` directory, which acts as the central repository for:

*   Building binaries for the host and for the release target platforms.
*   Running end-to-end (e2e) tests.
*   Generating documentation from the codebase.
*   Generating and publishing JSON schemas from Go types.

The entry point of the automation layer is `magefiles/core/core.go`, which bootstraps the runtime and exposes exported functions as Makefile-like executable targets.

## Namespace Architecture

Mage targets are organized into logical Go namespaces to group related operations together. Every target is invoked as `mage <namespace>:<target>`, and target names are case-insensitive, so `mage generate:engineconfig` and `mage generate:engineConfig` are the same command. Run `mage -l` for the authoritative list on your checkout.

### `Build` Namespace

The `Build` namespace is the default target and the build path for the Cargoship binary. It compiles natively with the host's Go toolchain:

*   `Binary` - Compiles the binary for the host's platform by default, or for a specified OS and architecture via `-os` and `-arch` flags.
*   `All` - Compiles all release binaries into `build/`.
*   `Examples` - Builds a package from every example definition using the cargoship binary on `PATH` (not a binary built here), exercising the same path a release user would take. Gigabytes per package and hours in total; one failure does not stop the run.

```sh
mage build:binary                        # build for this host's OS/arch
mage build:binary -os=darwin -arch=arm64 # build build/cargoship_darwin_arm64
mage build:all                           # build every release binary into build/
mage build:examples                      # build every example package with cargoship on PATH
mage                                     # same as `mage build:all` -- it is the default target
```

### `Dev` Namespace

The `Dev` namespace aggregates convenience tasks for day-to-day development:

*   `Clean` - Deletes local compilation artifacts and cleans the `build/` directory.
*   `Tidy` - Runs `go mod tidy` inside the workspace.
*   `Vendor` - Runs `Tidy`, then `go mod vendor`, then `WriteOSVOverrides`. Use this rather than a bare `go mod vendor` after changing dependencies: `go.mod`, `go.sum`, and `vendor/` are updated in one step, and the generated `osv-scanner.toml` files that `go mod vendor` deletes are put back.
*   `WriteOSVOverrides` - Writes those generated `osv-scanner.toml` files into `vendor/`. `Vendor` calls it, so run it on its own only to repair a tree that was vendored some other way - usually a Dependabot branch, since Dependabot re-vendors without going through mage.
*   `VerifyVendor` - Checks that every declared override is present and still matches the generator, and that `vendor/` holds no non-Go dependency manifest without an entry covering it. `.github/workflows/check-go-mod.yaml` runs it on every pull request. [Why vendor/ carries four generated osv-scanner.toml files](../agent/choice-osv-vendor-overrides.md) explains what the files are for.
*   `Digest` - Resolves `docker.io/library/alpine:latest` through the host's Docker credentials and prints its digest. This is a connectivity and auth smoke test, not part of a build.
*   `DnfPins` - Queries the AlmaLinux 10 RPM repository metadata over HTTP to find the newest available versions of the packages installed in the container images (`ansible-core` and `bash-completion`), then updates `containers/ansible/Dockerfile` and `containers/ubi/Dockerfile` (via `ARG` defaults) and `.goreleaser.yaml` (via `build_args`). A package is queried only while an image installs it: `shadow-utils` was dropped when the ubi image stopped installing it, because a pin nothing consumes makes this target fail looking for an `ARG` that is not there. `TestUpdatersMatchTheRepositoryFiles` holds the updaters against the real files for that reason.

```sh
mage dev:clean                  # rm the build/ artifacts
mage dev:tidy                   # go mod tidy
mage dev:vendor                 # go mod tidy, go mod vendor, then rewrite the osv overrides
mage dev:digest                 # print the alpine:latest digest, to check registry auth works
mage dev:writeOSVOverrides      # write every osv-scanner.toml override into vendor/
mage dev:verifyVendor           # check those overrides are present, current, and complete
mage dev:dnfPins                # query AlmaLinux repodata and bump dnf/microdnf package pins
```

### `Test` Namespace

The `Test` namespace hosts the integration and validation suites:

*   `Unit` - Runs `go test` over every package except the e2e suites under `test/`, which need Docker and runners of their own. Needs no binary, cluster, or network, and is what the `unit` CI job runs.
*   `EndToEnd` - Runs the whole e2e suite: both the cluster and non-cluster groups, including the example packages that pull ~1.5GB of engine artifacts and images. Needs Docker.
*   `EndToEndNonCluster` - Runs the group that needs no cluster: the misc and package command groups. `-short` additionally skips the example packages, so this finishes in seconds. Mirrors the `e2e-noncluster` CI job.
*   `EndToEndCluster` - Runs only the group that needs a bootloose cluster: the install command group. Needs Docker. It builds nothing; that suite calls the cargoship packages directly rather than driving a binary.
*   `EndToEndClusterStage` - Runs the same suite as `EndToEndCluster`, but stops at the boundary phase/60 draws: it stages the files and renders the engine config without starting the engine on any node. It provisions a different five machines rather than fewer: `stageOS` is one per OS family per role, including a Fedora controller and the upload-only Alpine host, where the full walk's `k3sOS` is a single Ubuntu controller and four workers.
*   `EndToEndClusterDryRun` - Runs only the dry-run walk of the cluster suite: a whole apply, prepare and reset with `--dry-run` against the staging cluster, asserting the hosts are left as they were found and that the read-only phases still gathered their facts. Starts no engine, so it is the fastest way to exercise the flag against real hosts.
*   `CleanCluster` - Removes the containers a bootloose cluster left behind. `EndToEndCluster` does this before it runs; use this target for a run that was killed partway through, or to inspect what a failed run left before clearing it.
*   `Fuzz` - Replays the fuzz seed corpus (the `f.Add` values in each target plus anything committed under `fuzz/testdata/fuzz/<Target>/`). Calls the packages in process; needs no binary, cluster, or network. See [fuzz-tests](fuzz-tests.md).

```sh
mage test:endToEnd              # build the binary, then run both e2e groups (needs Docker)
mage test:endToEndNonCluster    # run the misc/package command suites, no cluster needed
mage test:endToEndCluster       # run the install command suite against a bootloose cluster
mage test:endToEndClusterStage  # same, but stop at the pre-engine staging boundary
mage test:endToEndClusterDryRun # run only the --dry-run walk against the staging cluster
mage test:cleanCluster          # remove bootloose containers left behind by a killed run
mage test:fuzz                  # replay the fuzz seed corpus
```

## File-by-File Reference

*   **`core/core.go`:** Configures the bootstrap process and imports distro-specific modules to register Go side-effects before task execution. Lives in its own subpackage (rather than directly in `magefiles/`) so it does not collide with the `func main()` that the `mage` CLI generates on the fly - see [Running Mage Directly](#running-mage-directly-without-the-cli) below.
*   **`build.go`:** Entrypoint for the `Build` namespace (`Build.Binary`, `Build.All`, `Build.Examples`), delegating to `magefiles/pkg/build`.
*   **`dev.go`:** Entrypoint for the `Dev` namespace (`Dev.Clean`, `Dev.Tidy`, `Dev.Vendor`, `Dev.Digest`, `Dev.DnfPins`, `Dev.WriteOSVOverrides`, `Dev.VerifyVendor`).
*   **`generate.go`:** Entrypoint for the `Generate` namespace (`Generate.Document`, `Generate.Schema`, `Generate.Completion`, `Generate.EngineConfig`, `Generate.Examples`, etc.), delegating to `magefiles/pkg/gen/...`.
*   **`test.go`:** Entrypoint for the `Test` namespace (`Test.Unit`, `Test.EndToEnd`, `Test.EndToEndNonCluster`, `Test.EndToEndCluster`, `Test.EndToEndClusterStage`, `Test.EndToEndClusterDryRun`, `Test.EndToEndClusterUpgrade`, `Test.EndToEndZarf`, `Test.CleanCluster`, `Test.CleanZarfClusters`, `Test.Fuzz`), delegating to `magefiles/pkg/testrunner`.
*   **`pkg/build/`:** Binary compilation logic, flag assembly, and example package builds.
*   **`pkg/devtools/`:** Developer tooling packages, including `dnfpins` (AlmaLinux repomd XML parser and pin updater) and `osv` (OpenSSF Scorecard vendor overrides).
*   **`pkg/gen/`:** Generator implementations: `completion/` (shell completion scripts), `docs/` (Cobra command docs and mdBook pages), `engineconfig/` (k3s/RKE2 source pins and struct codegen), `examples/` (Rancher and upstream distro examples), and `schema/` (JSON schema reflection and docs).
*   **`pkg/testrunner/`:** Test runner implementations for e2e suites and fuzz corpus replay.
*   **`pkg/util/`:** Shared helpers for aligned table formatting, git commit resolution, and clean build tasks.
*   **`templates/`:** Text templates the generation targets render: `rke2-distro.yaml.tmpl`, `k3s-distro.yaml.tmpl`, and `upstream-distro.yaml.tmpl` (rendered once per CNI flavor), plus the per-distro values templates a flavor ships beside its `distro.yaml` (`rke2/values.yaml.tmpl`, `rke2/values.schema.json.tmpl`, and the `k3s/` equivalents).

---

## Managed Outputs

Running various Mage tasks maintains and updates the following filesystem artifacts:

| Output Directory / File                           | Description                                                                                 | Target                                             |
| :------------------------------------------------ | :------------------------------------------------------------------------------------------ | :------------------------------------------------- |
| `build/cargoship_*`                               | Compiled release binaries                                                                   | `Build.All`                                        |
| `docs/commands/*`                                 | Auto-generated CLI documentation                                                            | `Generate.Document`                                |
| `docs/phases/*`                                   | Auto-generated cluster phase descriptors                                                    | `Generate.Document`                                |
| `docs/schema/*`                                   | Auto-generated schema field reference, from the same struct reflection as `schema/*.json`   | `Generate.Document`                                |
| `docs/ansible/<collection>/module_*.md`           | Auto-generated Ansible module reference, from the action plugins                            | `Generate.Document`                                |
| `docs/ansible/<collection>/role_*.md`             | Auto-generated Ansible role reference, from each role's `meta/argument_specs.yml`           | `Generate.Document`                                |
| `docs/index.md`                                   | The root `README.md`, with its links rebased onto `docs/` for the book                      | `Generate.Document`                                |
| `docs/security.md`                                | `.github/SECURITY.md`, with its links rebased onto `docs/` for the book                     | `Generate.Document`                                |
| `docs/SUMMARY.md`                                 | Compiled table of contents for mdBook                                                       | `Generate.Document`                                |
| `schema/*.json`                                   | JSON schemas for YAML validations                                                           | `Generate.Schema`                                  |
| `hack/completion/*`                               | Shell completion scripts for bash, zsh, fish, and powershell                                | `Generate.Completion`                              |
| `pkg/engineconfig/gen/*`                          | Typed engine `config.yaml` structs per distro/version                                       | `Generate.EngineConfig`                            |
| `thirdparty-src/<distro>/<minor>/*`               | Raw pinned upstream k3s/RKE2 source                                                         | `Generate.PullEngineSource` / `Generate.LatestTag` |
| `thirdparty-src/pins.json`                        | Pinned upstream tags                                                                        | `Generate.LatestTag` / `Generate.UpdatePins`       |
| `example/<distro>-<cni>/<minor>/*/distro.yaml`    | Rendered rke2 and k3s example packages, one directory per CNI flavor, grouped by minor line | `Generate.Examples`                                |
| `example/shasums.json`                            | Cached sha256 of every remote file the examples hash                                        | `Generate.Examples` / `Generate.ExampleLine`       |
| `<zarf_cache>/examples/*`                         | Cached release text assets (image lists), not committed                                     | `Generate.Examples` / `Generate.ExampleLine`       |
| `containers/ansible/Dockerfile`                   | Base image package pins for ansible-core and bash-completion                                | `Dev.DnfPins`                                      |
| `containers/ubi/Dockerfile`                       | Base image package pin for bash-completion                                                  | `Dev.DnfPins`                                      |
| `.goreleaser.yaml`                                | Build args pinning container package versions                                               | `Dev.DnfPins`                                      |

---

## Running Mage Directly (Without the CLI)

Normally you invoke tasks through the installed `mage` binary, e.g. `mage build:binary`. The `mage` CLI works by scanning `magefiles/` for exported functions and namespaces, then generating its own `func main()` (written to a gitignored `mage_output_file.go`) that wires those functions up to CLI subcommands before compiling and running the result.

Because that generated file declares `package main` with its own `func main()`, it cannot coexist with a second, hand-written `func main()` in the same package - hence `core/core.go` (which does exactly that, via `mage.Main()`) is split out into its own `magefiles/core` subpackage rather than sitting alongside the task files in `magefiles/`.

This split means `core/core.go` can also be run on its own, bypassing the `mage` CLI entirely:

```sh
go run ./magefiles/core
```

This builds and runs the same `mage.Main()` entry point that the `mage` CLI would otherwise generate for you. It's useful when:

*   The `mage` binary isn't installed on the host (e.g. a minimal CI or container image that already has a Go toolchain). This is how CI runs its document checks - `go run -mod=vendor ./magefiles/core generate:document` - so the workflow needs nothing beyond `setup-go` and the vendored tree.
*   You want a single, explicit `go run` invocation instead of depending on a separately-installed tool.

Task selection still works the same way - pass the namespace:target as an argument, e.g.:

```sh
go run ./magefiles/core build:binary
```

Note that `magefiles/` itself remains its own `package main` for the `mage` CLI's benefit; `core/core.go` is a separate package and binary, not part of that compiled unit.
