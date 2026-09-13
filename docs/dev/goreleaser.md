# GoReleaser Release Pipeline

This document describes how Cargoship turns a pushed tag into published artifacts: archives, Linux packages, container images, and signatures. The pipeline is defined by `.goreleaser.yaml` at the repository root and driven by `.github/workflows/release.yaml`.

This is the *release* path. It is distinct from the [Dagger](dagger.md) and [Mage](mage.md) pipelines, which build binaries for local development and CI checks.

## Trigger

The workflow runs on any pushed tag. Tags are normally created by release-please, so the practical sequence is: merge the release PR, release-please pushes the tag, the goreleaser workflow fires.

## What Gets Published

A single `goreleaser release` run produces:

*   **Archives** — `tar.gz` per OS/arch, named after `uname` conventions (`cargoship_Linux_x86_64.tar.gz`).
*   **Packages** — `apk`, `deb`, and `rpm`, from the `nfpms` section, GPG-signed when `GPG_KEY_PATH` is set.
*   **Container images** — a multi-platform `ghcr.io/colonel-byte/cargoship:<tag>` manifest covering `linux/amd64` and `linux/arm64`.
*   **SBOMs** — one per archive, via Syft.
*   **Signatures** — cosign `sigstore.json` bundles over both the checksums file and every archive.

## Build Matrix

The `builds` section compiles `linux` and `darwin` against `amd64`, `arm64`, and `riscv64`. Three settings there matter more than they look:

*   **`CGO_ENABLED=0`** — produces a statically linked binary. This is what lets the container image use an Alpine (musl) base; a glibc-linked binary fails there with a confusing `no such file or directory` on exec.
*   **`ldflags -X`** — stamps `config.CLIVersion` and `config.CLICommit`, which is what `cargoship version` prints.
*   **`mod_timestamp: {{ .CommitTimestamp }}`** — pins file mtimes to the commit for reproducibility. `nfpms.mtime` and the image's `org.opencontainers.image.created` label use `.CommitDate` for the same reason.

## Container Images

Images are configured under `dockers_v2`, which replaced the older `dockers` + `docker_manifests` pair. GoReleaser emits every platform and the manifest list that fans out to them from a single `docker buildx build --push`.

Three consequences are worth internalizing:

1.  **Images build during the publish phase**, not the build phase. `goreleaser build` and `goreleaser release --skip=publish` never touch them. Use `--snapshot` to exercise the Dockerfile.
2.  **A multi-platform-capable buildx builder is required.** The default `docker` driver cannot produce a manifest, hence the `setup-buildx-action` step in the workflow.
3.  **Snapshots emit per-platform tags** (`:<tag>-amd64`, `:<tag>-arm64`) rather than a manifest, because buildx cannot create a manifest without pushing. Real releases publish the single manifest tag.

`dockers_v2` is still flagged experimental upstream and prints a warning on every run. It becomes plain `dockers` in GoReleaser v3.

### The Dockerfile Contract

The root `Dockerfile` only ever *copies* — it never compiles. GoReleaser has already built the binaries by the time it runs, and adding a builder stage would duplicate that work and break the guarantee described under [Binary Identity](#binary-identity).

`dockers_v2` hands the Dockerfile a temporary context laid out one directory per platform, which is why the binary is copied from `$TARGETPLATFORM` rather than the context root:

```
<context>/Dockerfile
<context>/linux/amd64/cargoship
<context>/linux/arm64/cargoship
```

The source tree is **not** in that context. Anything else the image needs must be declared in `extra_files`.

Every `RUN` in the Dockerfile lives in a stage pinned to `--platform=$BUILDPLATFORM` so it executes natively once, on the runner's own architecture, rather than once per target under emulation. Those stages emit only arch-independent text (`/etc/passwd`, directory skeletons) that is safe to copy into any target's final image. Keep it that way when editing.

### Image Runtime Layout

*   Runs as uid/gid `65532` with a real `/etc/passwd` entry, so `$HOME` and `os/user` lookups resolve.
*   `HOME=/home/cargoship`, which backs viper's `$HOME/.zarf` config search path (`src/cmd/viper.go`) and the default cache path `~/.cargoship-cache` (`src/config/common.go`).
*   `WORKDIR /workspace`. Since `.` is viper's first config search path, bind-mounting a package directory there picks up `cargoship-config.yaml` with no extra flags:

```sh
docker run --rm -v "$PWD:/workspace" ghcr.io/colonel-byte/cargoship:<tag> apply ...
```

### OCI Labels

Labels are split by whether they change per release. Static ones (title, description, source, licenses, base image) live in the `Dockerfile`; per-release ones (version, revision, created) are set in `.goreleaser.yaml`. Keep them separate — a `--label` flag silently overrides a `LABEL` of the same key, so duplicating a key across both files creates two sources of truth.

## Binary Identity

The binary inside the container image is byte-identical to the one in the release archive. GoReleaser compiles once per target and reuses that artifact for the archive, the nfpm packages, and the Docker build context.

This means a cosign verification against the released archive transitively covers the binary shipped in the image. Verify it yourself after a snapshot run:

```sh
sha256sum dist/cargoship_linux_amd64_v1/cargoship
cid=$(docker create ghcr.io/colonel-byte/cargoship:<tag>-amd64)
docker cp "$cid:/usr/local/bin/cargoship" /tmp/from-image && docker rm "$cid"
sha256sum /tmp/from-image
```

The property holds only because the Dockerfile has no builder stage.

## CI Workflow Steps

Each setup step in `.github/workflows/release.yaml` exists for a specific reason:

*   **Syft** — generates the archive SBOMs.
*   **cosign** — signs checksums and archives; needs `COSIGN_PRIVATE_KEY` and `COSIGN_PASSWORD`.
*   **QEMU** — registers binfmt handlers for cross-platform builds. Currently headroom rather than a hard dependency, since no `RUN` targets a foreign platform.
*   **Buildx** — required, not optional; provisions the container driver that can emit a multi-platform manifest.
*   **GHCR login** — required to push. The job's `packages: write` permission is an authorization, not a credential.
*   **`fetch-depth: 0`** — GoReleaser needs full history to resolve tags and changelog range.

## Local Testing

Validate the config without building anything:

```sh
goreleaser check
```

Run the whole pipeline locally without publishing. Skip the signing and SBOM stages unless you have cosign keys in your environment:

```sh
TMPDIR=/home/$USER/.cache/goreleaser-tmp \
  goreleaser release --snapshot --clean --skip=sign,sbom,announce,validate,before
```

Two gotchas:

*   **Set `TMPDIR` to a path on a real disk.** The default `/run/user/<uid>/tmp` is a small tmpfs, and the Go build cache for a full six-target matrix will overrun it. The failure is a flood of `no space left on device` from the compiler.
*   **Do not add `--skip=publish`** if you want images. Under `dockers_v2` that skips image builds entirely.

Artifacts land in `dist/`, which is gitignored.
