# Importing Archives Guide

This guide explains how to convert external image archives—specifically Skopeo `oci-archive`, `docker-archive`, or OCI layout directories—into native Cargoship packages (`.tar.zst`).

## The Air-Gap Workflow

In an air-gapped workflow, packages are often published to an OCI registry on an internet-connected staging network, then copied to removable media using container tools like Skopeo.

Because Cargoship packages are multi-platform artifacts whose OCI manifest lists record the platform OS as `"multi"` (inherited from the OCI packaging specification), pass `--override-os multi` when pulling with Skopeo so its platform filter matches:

```sh
# Export package from OCI registry as an oci-archive
skopeo copy --override-os multi \
  docker://registry.example.com/cargoship/my-package:1.0.0 \
  oci-archive:my-package.tar:1.0.0
```

> **Note on `docker-archive`**: Cargoship packages contain custom artifact configs (`application/vnd.cargoship.config.v1+json`). Docker daemon archives only support standard container image config formats; attempting `skopeo copy ... docker-archive:` against a published Cargoship package will fail with `unsupported image-specific operation on artifact with type "application/vnd.cargoship.config.v1+json"`. Always use `oci-archive:` when exporting Cargoship packages with Skopeo. `cargoship import --format=docker-archive` remains available for standard container image tarballs exported via `docker save`.

Once transferred across the airlock into the disconnected environment, `cargoship import` converts the archive into a native Cargoship package archive that `cargoship apply` can install.

## Basic Usage

Run `cargoship import` on the archive:

```sh
cargoship import my-package.tar
```

By default, the command:
1. Detects whether the input is an `oci-archive` (OCI Image Layout) or `docker-archive` (Docker save layout).
2. Extracts and verifies all package layers (binaries, engine configurations, container images, and checksums).
3. Writes the native compressed archive (`cargoship-<name>-<arch>-<version>.tar.zst`) into the current directory.

To write the native package to a specific directory, pass `-o, --output`:

```sh
cargoship import my-package.tar --output /srv/cargoship/packages/
```

## Explicit Format Selection

While format detection is automatic, you can enforce the format with `--format`:

```sh
# Import an OCI archive or OCI layout directory
cargoship import my-package.tar --format=oci-archive

# Import a Docker image archive (docker save format)
cargoship import docker-export.tar --format=docker-archive
```

The `--format` flag supports tab completion in bash/zsh with values `auto`, `oci-archive`, and `docker-archive`.

## Importing Unpacked Directories

If the archive has already been extracted on disk as an OCI layout directory (containing `oci-layout`, `index.json`, and `blobs/`), pass the directory path directly:

```sh
cargoship import ./extracted-oci-dir/ -o ./build/
```

## Verifying Signatures

If the package was signed before export, pass `--key` and `--verify` to validate its Cosign signature as part of the import:

```sh
cargoship import my-package.tar --verify=always --key ./cosign.pub
```
