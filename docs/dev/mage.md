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
*   `EndToEndClusterStage` - Runs the same suite as `EndToEndCluster`, but stops at the boundary phase/60 draws: it stages the files and renders the engine config without starting the engine on any node, and provisions five machines rather than ten.
*   `CleanCluster` - Removes the containers a bootloose cluster left behind. `EndToEndCluster` does this before it runs; use this target for a run that was killed partway through, or to inspect what a failed run left before clearing it.
*   `Fuzz` - Replays the fuzz seed corpus (the `f.Add` values in each target plus anything committed under `fuzz/testdata/fuzz/<Target>/`). Calls the packages in process; needs no binary, cluster, or network. See [fuzz-tests](fuzz-tests.md).

```sh
mage test:endToEnd              # build the binary, then run both e2e groups (needs Docker)
mage test:endToEndNonCluster    # run the misc/package command suites, no cluster needed
mage test:endToEndCluster       # run the install command suite against a bootloose cluster
mage test:endToEndClusterStage  # same, but stop at the pre-engine staging boundary
mage test:cleanCluster          # remove bootloose containers left behind by a killed run
mage test:fuzz                  # replay the fuzz seed corpus
```

### `Generate` Namespace

The `Generate` namespace handles code-generation and repository asset updates:

*   `Document` - Automatically generates command documentation from Cobra structures, parses cluster operational phase descriptors, renders godoc comments in `pkg/`, `api/`, and `types/` into `docs/golang/` with `gomarkdoc`, renders `docs/schema/` from the same struct reflection `Schema` feeds to `schema/*.json`, renders the Ansible collection's module and role reference pages from the action plugins' `DOCUMENTATION` blocks and the roles' `meta/argument_specs.yml`, rebases `README.md` into `docs/index.md` and `.github/SECURITY.md` into `docs/security.md` for the book, and formats the mdBook `docs/SUMMARY.md` structure.
*   `Schema` - Generates YAML-compatible JSON schemas in `schema/` from Go structs using reflection, facilitating IDE autocomplete and validation for cluster config, distro packages, and runtime configs.
*   `Completion` - Generates shell tab completion scripts for bash, zsh, fish, and powershell into `hack/completion/`.
*   `PullEngineSource` - Fetches raw k3s/RKE2 source at the tags pinned in `thirdparty-src/pins.json` into `thirdparty-src/` (see [thirdparty-src](thirdparty-src.md)). Touches the network.
*   `LatestTag <distro> <vMAJOR.MINOR>` - Resolves the newest non-RC upstream tag for that minor line, pins it in `thirdparty-src/pins.json`, and re-pulls that version's source if the pin moved. Touches the network.
*   `UpdatePins` - Runs `LatestTag` over every minor line already pinned in `thirdparty-src/pins.json`, refreshing each to its newest patch release. Touches the network.
*   `Examples` - Renders `magefiles/templates/<distro>-distro.yaml.tmpl` into `example/<distro>-<cni>/<minor>/v<version>/distro.yaml`, one flavor per CNI (`example/k3s-flannel/`) plus a multi-architecture flavor per CNI (`example/rke2-multi-cni-canal/`, `example/rke2-multi-cni-cilium/`, `example/k3s-multi/`) and single-purpose flavors that vary one setting (`example/rke2-cilium-vsphere/`), grouped by minor line (`v1_35/`, matching `thirdparty-src/<distro>/` and `pkg/engineconfig/gen/<distro>/`) and creating those directories as needed. Per flavor it renders once per tag of that distro pinned in `thirdparty-src/pins.json` and once per example directory that flavor already has on disk, so a template edit reaches the older examples instead of leaving them to drift. Everything that varies between versions is derived from the tag, except `imageConfig.images` and the digests, which come from that release's published assets. Touches the network the first time it renders a version: each release's text assets are cached under `<zarf_cache>/examples/`, one flat file per asset URL named the way phase 50 names an image tarball from an image reference, so re-rendering a version already on disk fetches nothing. An entry is only used while the URL it was fetched from still matches, and `CARGOSHIP_EXAMPLES_NO_CACHE=1` refetches everything for a run - needed when Rancher re-cuts a release's assets in place, since the URL does not change when it does.

    Distros and their flavors are declared together in `exampleDistros` in `magefiles/pkg/gen/examples/distros.go`. Flavors differ by more than their image list: cilium replaces kube-proxy (`disable-kube-proxy: true`) and is configured through an `rke2-cilium` HelmChartConfig manifest, while canal and flannel run alongside kube-proxy and carry no manifest of their own. Adding a CNI means adding a flavor entry there, and a `{{ if }}` in that distro's template for anything specific to it.

    A flavor also names values templates, which are rendered next to `distro.yaml` under the same name minus `.tmpl`. They are grouped per distro rather than per flavor -- `magefiles/templates/rke2/` and `magefiles/templates/k3s/` -- because a package carries a single schema, so everything its flavors expose has to be described in one document. The templates are rendered against the same `exampleVersion` the definition is, so a `{{ if eq .CNI "cilium" }}` in them covers what only the cilium flavors expose. Those values are what let one example stand in for several: which bundled charts the engine installs, and for cilium encryption, L2 announcements, and the Hubble UI, are chosen at install time through the cluster's `spec.config.values` rather than baked into a separate flavor at render time.

    The rendered schema suggests `addons.disabled` values from `exampleVersion.Addons`, which is that distro/minor's packaged-component vocabulary as `EngineConfig` extracted it -- so a v1.37 example offers `rke2-gateway-api-crd` and a v1.34 one does not. They are `examples` rather than an `enum`: the vocabulary comes from the engine's `--disable` help text, which does not name every chart the build bundles, so restricting to it would reject charts the engine would have honored. A version whose source has never been pulled has no vocabulary, and rendering its values files fails rather than writing a values file an editor can offer nothing for. Pull that minor line (`Generate.PullEngineSource`) and re-run `EngineConfig` first.

    A flavor also decides which architectures its examples target and which releases it covers. `arches` lists the architectures, defaulting to amd64 alone; `minors` limits the flavor to named minor lines, defaulting to every line the distro renders; and `name` is what follows the distro in `metadata.name` when the CNI is not the point. The multi-architecture flavors use all three: they target amd64 and arm64, cover only `v1_36`, and are named `multi`, because they exist to show what a package covering several architectures looks like rather than to cover every release. Everything a distro publishes once per architecture -- RKE2's versioned RPMs and release tarball, the k3s binary and its digest -- hangs off `Arches` on the rendered version rather than off the version itself, so the templates range over architectures whether there is one or several, and a single-architecture example renders exactly as it did before, without an `arch` selector on anything.

    What differs between the distros themselves sits in three hooks on the distro entry, so the rendering itself stays single-copy: `derive` builds the URLs only that distro has, `probe` names the URL whose absence retires a build, and `fetch` pulls anything else only the release can answer. RKE2 installs versioned `rke2-common`/`server`/`agent` RPMs from `rpm.rancher.io` and splits its images across a core manifest plus one per CNI. k3s installs a single binary straight from its GitHub release, ships flannel inside that binary - so `k3s-images.txt` is the whole list and there is nothing per-CNI to fetch - and publishes `sha256sum-<arch>.txt`, which is where each binary's `shasum` comes from rather than downloading 70 MB per version and architecture to hash it. Both distros install their distro's selinux policy RPM.

    k3s examples share one copy of the unit files and killall script, kept in `example/k3s/core/` and referenced as `../../../k3s/core/…`; relative `source` paths resolve against the directory holding `distro.yaml`.

    Each RPM entry also carries a `shasum`, so cargoship can verify the file it downloads. Hashing an RPM means downloading it, and `rke2-common` alone is around 29 MB, so the digests are cached in `example/shasums.json`. It is a committed map keyed by file name, each entry holding the `urls` it has been fetched from and its `sha256`, so the file reads as an inventory of what the examples install. Only files missing from it are fetched, which makes a re-render free and a new release cost just its own four RPMs. An entry whose URL is not one already recorded against that digest is re-hashed rather than trusted, so moving a file cannot hand back a stale digest. Delete an entry to force it to be re-hashed.

    A file extracted out of an archive is keyed by the archive and the path inside it, joined by `!` - `v1.37.0%2Brke2r1/rke2.linux-amd64.tar.gz!bin/rke2`. That is a separate entry from the archive itself, because a `files` entry that sets `extractPath` declares the digest of the file it installs, and the archive it came out of hashes to something else. The archive is named by the last two segments of its URL rather than by its file name alone, which a whole-file entry is enough for: every rke2 release publishes its tarball as `rke2.linux-amd64.tar.gz`, so a key built from that name would be the same for all seventy of them, each render would evict the last, and every run would re-download every tarball. The segment above the file name is the release tag, which is what makes the key specific to a version.

    RKE2's binary, its killall and uninstall scripts, and both unit files all come out of `rke2.linux-<arch>.tar.gz`, so the template asks for five members of each tarball. One streaming pass hashes every file in the archive and the ones asked for are cached individually, so a tarball is pulled only when the cache is missing at least one of its members, and pulled once when it is. Nothing is written to disk. A new rke2 release costs its two tarballs - around 80 MB - on top of its RPMs; a re-render of what is already cached costs nothing.

    Before rendering, each build's `rke2-common` RPM is checked (a `HEAD`, and only for URLs the cache has never seen). Rancher supersedes an `rke2rN` with the next revision and removes the old RPMs, while the git tag and image manifests stay up - so a build can look renderable and still install nothing. Those are skipped, and any example already on disk for one is deleted, along with its minor line directory if that empties it. As of August 2026 that covers `v1.35.0+rke2r2` and `v1.35.3+rke2r2`, replaced by `rke2r3`, and `v1.34.3+rke2r2` and `v1.34.6+rke2r2` - use the `rke2r3` release of those patches. Nothing is remembered about a skip, so a build that comes back is rendered again on the next run; a build that goes away is only noticed while it is still pinned or still on disk. The `shasum` on an individual file is still allowed to be missing - that path now only covers the odd file a still-published build lost.

*   `ExampleLine <distro> <vMAJOR.MINOR>` - Renders an example for *every* non-RC release on one minor line of that distro, rather than only the pinned one, so `rke2 v1.36` backfills `v1.36.0+rke2r1` through the newest `v1.36` release into `example/rke2-multi-cni-cilium/v1_36/`, `example/rke2-cilium-vsphere/v1_36/`, and `example/rke2-multi-cni-canal/v1_36/`. A flavor that names minor lines is only rendered for the lines it names, so asking for a line a multi-architecture flavor does not cover renders the other flavors alone. The leading `v` is optional. Once written, `Examples` keeps those files current, since it re-renders every example directory on disk. Touches the network, through the same release-asset cache `Examples` uses.

    `ExampleLine upstream <vMAJOR.MINOR>` is a separate path (see `magefiles/pkg/gen/examples/upstream.go` and its `upstream_kubeadm.go`, `upstream_deb.go`, `upstream_rpm.go`, and `upstream_repo.go` siblings), since kubeadm does not fit `exampleDistroSpec`. It backfills every stable patch on the line by picking, per package, the closest version published on `pkgs.k8s.io` at or before that patch - pkgs.k8s.io keeps every patch's packages on a minor line, not just the newest, so `v1.37.0` and `v1.37.1` render distinct `kubelet`/`kubectl`/`kubeadm`/`cri-tools`/`kubernetes-cni` versions. The etcd, coreDNS, and pause versions kubeadm pins come from that same tag's own `cmd/kubeadm/app/constants/constants.go`, fetched live from `raw.githubusercontent.com`, not from the single per-minor cached copy `thirdparty-src/upstream/<minor>/` holds - that copy only ever reflects whichever tag is currently pinned. `containerd.io`, pulled from `download.docker.com`, is the one package still resolved to whatever is newest at render time, since its release cadence is independent of the Kubernetes tag.
*   `EngineConfig` - Statically parses raw engine source under `thirdparty-src/` (see [thirdparty-src](thirdparty-src.md)) to generate typed `config.yaml` structs per distro/version in `pkg/engineconfig/gen/`, plus each version's packaged-component vocabulary (the valid `disable:`, `cni:`, and `ingress-controller:` values) in a `zz_addons.go` alongside them.

```sh
mage generate:document                  # regenerate docs/commands, docs/phases, docs/golang, docs/schema, docs/ansible, docs/index.md, docs/security.md, and docs/SUMMARY.md
mage generate:schema                    # regenerate schema/*.json from the Go API types
mage generate:completion                # regenerate hack/completion/ shell completion scripts (bash, zsh, fish, powershell)

mage generate:pullEngineSource          # re-pull every tag already pinned in thirdparty-src/pins.json
mage generate:latestTag rke2 v1.36      # pin rke2's newest v1.36.x, and pull it if the pin moved
mage generate:latestTag k3s v1.31       # same, for a k3s minor line
mage generate:updatePins                # bump every pinned minor line, both distros, to its newest patch
mage generate:engineConfig              # regenerate pkg/engineconfig/gen/ from what is on disk
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

The usual order after any pin change is `updatePins` (or `latestTag`), then `engineConfig`, then `examples` - the first two write what the next one reads, and only the pin-moving targets touch the network for source. `engineConfig` is fully offline.

---

## File-by-File Reference

*   **`core/core.go`:** Configures the bootstrap process and imports distro-specific modules to register Go side-effects before task execution. Lives in its own subpackage (rather than directly in `magefiles/`) so it does not collide with the `func main()` that the `mage` CLI generates on the fly - see [Running Mage Directly](#running-mage-directly-without-the-cli) below.
*   **`build.go`:** Entrypoint for the `Build` namespace (`Build.Binary`, `Build.All`, `Build.Examples`), delegating to `magefiles/pkg/build`.
*   **`dev.go`:** Entrypoint for the `Dev` namespace (`Dev.Clean`, `Dev.Tidy`, `Dev.Digest`, `Dev.DnfPins`, `Dev.WriteOSVOverrides`, `Dev.VerifyVendor`).
*   **`generate.go`:** Entrypoint for the `Generate` namespace (`Generate.Document`, `Generate.Schema`, `Generate.Completion`, `Generate.EngineConfig`, `Generate.Examples`, etc.), delegating to `magefiles/pkg/gen/...`.
*   **`test.go`:** Entrypoint for the `Test` namespace (`Test.Unit`, `Test.EndToEnd`, `Test.EndToEndNonCluster`, `Test.EndToEndCluster`, `Test.EndToEndClusterStage`, `Test.EndToEndClusterUpgrade`, `Test.CleanCluster`, `Test.Fuzz`), delegating to `magefiles/pkg/testrunner`.
*   **`pkg/build/`:** Binary compilation logic, flag assembly, and example package builds.
*   **`pkg/devtools/`:** Developer tooling packages, including `dnfpins` (AlmaLinux repomd XML parser and pin updater) and `osv` (OpenSSF Scorecard vendor overrides).
*   **`pkg/gen/`:** Generator implementations: `completion/` (shell completion scripts), `docs/` (Cobra command docs and mdBook pages), `engineconfig/` (k3s/RKE2 source pins and struct codegen), `examples/` (Rancher and upstream distro examples), and `schema/` (JSON schema reflection and docs).
*   **`pkg/testrunner/`:** Test runner implementations for e2e suites and fuzz corpus replay.
*   **`pkg/util/`:** Shared helpers for aligned table formatting, git commit resolution, and clean build tasks.
*   **`templates/`:** Text templates the generation targets render: `rke2-distro.yaml.tmpl`, `k3s-distro.yaml.tmpl`, and `upstream-distro.yaml.tmpl`, plus the per-distro values templates a flavor ships beside its `distro.yaml` (`rke2/values.yaml.tmpl`, `rke2/values.schema.json.tmpl`, and the `k3s/` equivalents).

---

## Managed Outputs

Running various Mage tasks maintains and updates the following filesystem artifacts:

| Output Directory / File                           | Description                                                                                 | Target                                             |
| :------------------------------------------------ | :------------------------------------------------------------------------------------------ | :------------------------------------------------- |
| `build/cargoship_*`                               | Compiled release binaries                                                                   | `Build.All`                                        |
| `docs/commands/*`                                 | Auto-generated CLI documentation                                                            | `Generate.Document`                                |
| `docs/phases/*`                                   | Auto-generated cluster phase descriptors                                                    | `Generate.Document`                                |
| `docs/golang/*`                                   | Auto-generated Go package reference, from godoc comments in `pkg/`, `api/`, `types/`        | `Generate.Document`                                |
| `docs/schema/*`                                   | Auto-generated schema field reference, from the same struct reflection as `schema/*.json`   | `Generate.Document`                                |
| `docs/ansible/module_*.md`                        | Auto-generated Ansible module reference, from the action plugins                            | `Generate.Document`                                |
| `docs/ansible/role_*.md`                          | Auto-generated Ansible role reference, from each role's `meta/argument_specs.yml`           | `Generate.Document`                                |
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
