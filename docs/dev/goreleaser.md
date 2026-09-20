# GoReleaser Release Pipeline

This document describes how Cargoship turns a pushed tag into published artifacts: archives, Linux packages, container images, and signatures. The pipeline is defined by `.goreleaser.yaml` at the repository root and driven by `.github/workflows/release.yaml`.

This is the *release* path. It is distinct from the [Mage](mage.md) pipeline, which builds binaries for local development and CI checks.

## Trigger

The workflow runs on any pushed tag. Tags are normally created by release-please, so the practical sequence is: merge the release PR, release-please pushes the tag, the goreleaser workflow fires.

## What Gets Published

A single `goreleaser release` run produces:

*   **Archives** — `tar.gz` per OS/arch, named after `uname` conventions (`cargoship_Linux_x86_64.tar.gz`).
*   **Packages** — `apk`, `deb`, and `rpm`, from the `nfpms` section, GPG-signed when `GPG_KEY_PATH` is set.
*   **Container images** — two multi-platform manifests covering `linux/amd64` and `linux/arm64`: `ghcr.io/colonel-byte/cargoship:<tag>` (Chainguard static, bare binary) and `ghcr.io/colonel-byte/cargoship:<tag>-ubi` (Red Hat UBI, installs the rpm).
*   **SBOMs** — one per archive, via Syft.
*   **Signatures** — cosign `sigstore.json` bundles over both the checksums file and every archive, plus registry signatures over both container images.

## Build Matrix

The `builds` section compiles `linux` and `darwin` against `amd64`, `arm64`, and `riscv64`. Three settings there matter more than they look:

*   **`CGO_ENABLED=0`** — produces a statically linked binary. This is what lets the default image sit on `cgr.dev/chainguard/static`, which ships no libc, no shell, and no package manager; a dynamically linked binary fails there with a confusing `no such file or directory` on exec.
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

Both Dockerfiles live under `containers/` — `containers/base/Dockerfile` and `containers/ubi/Dockerfile` — and each is named explicitly by its `dockers_v2` entry's `dockerfile:` key. Neither compiles anything: GoReleaser has already built the binaries and packages by the time they run. The static image copies the binary, UBI installs the rpm. Adding a *compiling* stage to either would duplicate that work and break the guarantee described under [Binary Identity](#binary-identity).

The directory name is deliberately generic: `containers/base/` is the plain-binary image whatever base it happens to sit on, so a future base swap does not force another rename. If it is ever renamed anyway, three other files have to move with it in the same commit — the `dockerfile:` key in `.goreleaser.yaml`, the lint matrix in `.github/workflows/scan-lint.yaml`, and the `directories:` list of the `docker` ecosystem in `.github/dependabot.yaml`.

`dockers_v2` hands the Dockerfile a temporary context laid out one directory per platform, which is why the binary is copied from `$TARGETPLATFORM` rather than the context root:

```
<context>/Dockerfile
<context>/linux/amd64/cargoship
<context>/linux/arm64/cargoship
```

The source tree is **not** in that context. Anything else the image needs must be declared in `extra_files`.

The UBI file is single-stage: a `FROM`, a `COPY` of the rpm, and `RUN`s that install it and lay out the unprivileged user's directories.

The static file has to be two-stage. `cgr.dev/chainguard/static` carries no shell and no package manager, so the runtime image cannot `mkdir`, `chmod`, or `chown` anything for itself. A small Alpine `skeleton` stage builds the directory tree under `/skeleton` and the runtime stage copies it in. That stage creates empty directories only — nothing architecture-specific — so it is pinned to `--platform=$BUILDPLATFORM` and never runs under emulation.

Copy the skeleton **at its root**, not directory by directory:

```dockerfile
COPY --from=skeleton /skeleton/ /
```

A `COPY` whose destination directory does not yet exist creates that directory root-owned and carries ownership only on the entries beneath it. `COPY --from=skeleton /skeleton/workspace /workspace` therefore produces a `root:root` `/workspace` that uid `65532` cannot write to — the failure surfaces at runtime, not at build time. Merging the tree into `/` preserves every directory's own uid, gid, and mode.

A `$BUILDPLATFORM` stage is not automatically cheaper, and speed is not why the split exists. On the older Alpine-based version of this file, hoisting its one `RUN` into a build-platform stage measured *slower* on a `--no-cache` `linux/arm64` build (~1.9s vs ~1.3s): busybox `adduser`/`mkdir` under QEMU cost less than an extra stage and its `COPY`s. The split is here because the runtime base cannot execute anything at all.

### Base Image Pinning

Every `FROM` pins its base to a digest, in `tag@sha256:...` form:

```dockerfile
FROM cgr.dev/chainguard/static:latest@sha256:bf639cba19ba56329e6907ac26a7afcdde57a80b6aa66d5100da6883196e6b82
```

The tag stays in the reference for readability; the digest is what actually resolves. Keep the digest a **manifest-list (index) digest**, not a per-platform one, or the `linux/arm64` build will fail to find its platform:

```sh
docker buildx imagetools inspect cgr.dev/chainguard/static:latest --format '{{ .Manifest.Digest }}'
```

Two conventions here exist to keep dependabot working, and both look redundant until you know why:

*   **The reference is repeated on every `FROM`** rather than hoisted into an `ARG`. Dependabot's Dockerfile parser reads literal `FROM` lines only — it has no `ARG` resolution, so `FROM ${BASE_IMAGE}` is invisible to it and the pin would never be refreshed. It is also not stage-aware, so aliasing (`FROM alpine@sha256:... AS base` then `FROM base`) is no better: the bare stage reference parses as an image and produces a spurious `base:latest` dependency.
*   **`org.opencontainers.image.base.name` carries the tag only**, without the digest. Dependabot rewrites `FROM` lines and nothing else, so a label holding the full pinned reference would silently drift out of date on the first automated digest bump. As written, digest refreshes need no edit; moving the tag itself (`3.22` → `3.23`) means updating the label by hand, which is the comment sitting above it in both files.

`.github/workflows/check-base-image-label.yaml` catches that hand edit when it is forgotten. On any pull request touching a `Dockerfile` it compares each final `FROM`, digest stripped, against that file's `base.name`, and comments on the pull request rather than failing the job — a stale label does not break the build, and the pull request bumping the `FROM` is where the fix belongs. The comparison is literal, so the label names the registry the `FROM` actually pulls from, mirror and all, rather than the upstream image that mirror copies.

One base needs more care than the others. **`cgr.dev/chainguard/static` publishes `:latest` and nothing else** on Chainguard's free tier, and old digests are garbage-collected as the tag moves. A stale pin there does not merely go out of date — it eventually stops resolving, and the build fails with `manifest unknown`. That is the failure to expect if this image breaks without the Dockerfile changing; refresh the digest with the command above.

`.github/dependabot.yaml` tracks both directories under its `docker` ecosystem, on the same daily schedule as the other ecosystems, which is what keeps that pin inside the retention window.

### Linting

Dockerfiles are linted by [hadolint](https://github.com/hadolint/hadolint) in two places:

*   **`.pre-commit-config.yaml`** — the `hadolint-docker` hook, which runs the linter in a container and so needs a working Docker daemon. It picks up any file pre-commit identifies as a Dockerfile, excluding `vendor/`.
*   **`.github/workflows/scan-lint.yaml`** — the `containers` job, a matrix over the two Dockerfile paths.

The CI job lists paths explicitly rather than globbing. The action's `recursive` mode searches from the repository root, which would pull in the vendored Dockerfiles under `vendor/` — several of which do not pass. **Add a new path to that matrix when adding an image variant**; the pre-commit hook picks it up on its own.

Default rules and the default `info` failure threshold apply; there is no `.hadolint.yaml`.

### UBI Variant

A second `dockers_v2` entry builds `containers/ubi/Dockerfile`, published as the `-ubi` tag on the same image repository. It exists for environments that want a Red Hat base and an installed, queryable package rather than a loose binary.

The difference is how the binary arrives. The static image copies it; the UBI image `rpm --install`s the same package produced by the `nfpms` section, so it is registered in the image's rpm database:

```sh
$ docker run --rm --entrypoint rpm ghcr.io/colonel-byte/cargoship:<tag>-ubi -q cargoship
cargoship-0.21.1-1.x86_64
```

`rpm -V cargoship` and `rpm -ql cargoship` work as well, which is usually the point of asking for a UBI build in the first place.

Details worth knowing before editing it:

*   **`ids: [packagers]`** selects the nfpms artifacts for the build context instead of the raw binary. All three formats share one nfpms id, so the `.apk` and `.deb` land in the context too; the Dockerfile globs `*.rpm` and ignores them.
*   **The install step must run on the target platform.** `rpm` unpacks an architecture-specific binary into the target rootfs, so this one could not be hoisted to `$BUILDPLATFORM` even if it were worth doing. It is the reason `setup-qemu-action` is a hard dependency, and why the UBI build is the slow one.
*   **`rpm -i`, not `microdnf install`.** The package declares no dependencies, so nothing needs resolving and the build stays hermetic — no repo access at image build time. Switch to `microdnf` if `nfpms.*.depends` ever gains an entry.
*   **A signed rpm logs a `NOKEY` warning** during the build, because the release signing key is not in the image keyring. It is a warning, not a failure.
*   **The binary lands in `/usr/bin`** (per `nfpms.rpm.prefixes`), not `/usr/local/bin` as in the static image. The entrypoints differ accordingly; everything else — the `nonroot` account at uid `65532`, `$HOME`, `/workspace` — is deliberately identical, so the two variants are drop-in swaps.

The UBI base is substantially larger: roughly 354MB versus 137MB for the static image on amd64.

### Image Runtime Layout

*   Runs as uid/gid `65532`, which has a real `/etc/passwd` entry named `nonroot` with home `/home/nonroot` in both images, so `os/user` lookups and `$HOME` agree with each other and across variants. The static image inherits that account from its base; the UBI image creates it to match. The `USER` line stays numeric (`65532:65532`) so the image still runs correctly under a `runAsUser` that ignores names.
*   `HOME=/home/nonroot`, which backs viper's `$HOME/.zarf` config search path (`src/cmd/viper.go`) and the default cache path `~/.cargoship-cache` (`src/config/common.go`). Both directories are pre-created and owned by `65532`, as is `~/.ssh` at 0700.
*   `WORKDIR /workspace`. Since `.` is viper's first config search path, bind-mounting a package directory there picks up `cargoship-config.yaml` with no extra flags:

```sh
docker run --rm -v "$PWD:/workspace" ghcr.io/colonel-byte/cargoship:<tag> apply ...
```

### OCI Labels

Labels are split by whether they change per release. Static ones (title, description, source, licenses, base image) live in each `Dockerfile`; per-release ones (version, revision, created) are set in `.goreleaser.yaml`. See [Base Image Pinning](#base-image-pinning) for why `org.opencontainers.image.base.name` deliberately omits the digest. Keep them separate — a `--label` flag silently overrides a `LABEL` of the same key, so duplicating a key across both files creates two sources of truth.

## Image Signing

`docker_signs` signs the published images with cosign, using the same `COSIGN_PRIVATE_KEY` / `COSIGN_PASSWORD` pair as the archive signatures — no extra workflow configuration is needed.

```yaml
docker_signs:
  - id: images
    cmd: cosign
    args: ["sign", "--key=env://COSIGN_PRIVATE_KEY", "${artifact}@${digest}", "--yes"]
    stdin: "{{ .Env.COSIGN_PASSWORD }}"
```

Two details:

*   **`artifacts` is deliberately unset.** Its default targets the images built by `dockers_v2`; the named values (`images`, `manifests`) address the deprecated `dockers` and `docker_manifests` pipes instead.
*   **`${artifact}@${digest}`, not `${artifact}`.** GoReleaser captures the digest from the push and signs that exact image, so a concurrent push to the same tag cannot end up signed by mistake.

Unlike the archive signatures, this produces no local file — cosign pushes the signature to the registry beside the image, which is why the pipe runs at the very end of publishing. Verify a released image with:

```sh
cosign verify --key cosign.pub ghcr.io/colonel-byte/cargoship:<tag>
```

This pipe only runs on a real publish. A `--snapshot` run pushes nothing, so it never exercises signing.

## Binary Identity

The binary is byte-identical everywhere it ships: the release archive, the apk/deb/rpm packages, the static image, and the UBI image (where it arrives via `rpm --install`). GoReleaser compiles once per target and reuses that one artifact downstream.

This means a cosign verification against the released archive transitively covers the binary shipped in the image. Verify it yourself after a snapshot run:

```sh
sha256sum dist/cargoship_linux_amd64_v1/cargoship
cid=$(docker create ghcr.io/colonel-byte/cargoship:<tag>-amd64)
docker cp "$cid:/usr/local/bin/cargoship" /tmp/from-image && docker rm "$cid"
sha256sum /tmp/from-image
```

The property holds only because neither Dockerfile compiles anything.

## CI Workflow Steps

Each setup step in `.github/workflows/release.yaml` exists for a specific reason:

*   **Syft** — generates the archive SBOMs.
*   **cosign** — signs checksums, archives, and the published images; needs `COSIGN_PRIVATE_KEY` and `COSIGN_PASSWORD`.
*   **QEMU** — registers binfmt handlers for cross-platform builds. Required by the UBI image, whose `rpm --install` unpacks architecture-specific files and so must run on the target platform; its `linux/arm64` half runs under emulation. The static image does not need it — its only `RUN` is pinned to `$BUILDPLATFORM`.
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
