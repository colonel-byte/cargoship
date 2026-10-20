# Importing Archives Guide

This guide explains how to convert an OCI Image Layout - a Skopeo `oci-archive` file, or an unpacked layout directory - into a native Cargoship package (`.tar.zst`) with `cargoship import`.

## The Air-Gap Workflow

In an air-gapped workflow, packages are often published to an OCI registry on an internet-connected staging network, then copied to removable media using container tools like Skopeo.

Because Cargoship packages are multi-platform artifacts whose OCI manifest lists record the platform OS as `"multi"` (inherited from the OCI packaging specification), pass `--override-os multi` when pulling with Skopeo so its platform filter matches:

```sh
# Export the package from an OCI registry as an oci-archive
skopeo copy --override-os multi \
  docker://registry.example.com/cargoship/my-package:1.0.0 \
  oci-archive:my-package.tar:1.0.0
```

Once transferred across the airlock into the disconnected environment, `cargoship import` converts the archive into a native Cargoship package archive that `cargoship apply` can install.

## Basic Usage

Run `cargoship import` on the archive:

```sh
cargoship import my-package.tar
```

The command:

1. Unpacks the archive, if it is not already a directory, and reads its `index.json`.
2. Selects the manifest matching the target architecture, descending into a nested index for a multi-arch package.
3. Extracts and verifies all package layers (binaries, engine configurations, container images, and checksums).
4. Writes the native compressed archive (`cargoship-<name>-<arch>-<version>.tar.zst`) into the current directory.

To write the native package to a specific directory, pass `-o, --output`:

```sh
cargoship import my-package.tar --output /srv/cargoship/packages/
```

## Importing Unpacked Directories

If the archive has already been extracted on disk as an OCI layout directory (containing `oci-layout`, `index.json`, and `blobs/`), pass the directory path directly:

```sh
cargoship import ./extracted-oci-dir/ -o ./build/
```

## Other Tools That Write an OCI Layout

Any tool that writes an OCI Image Layout can feed `cargoship import`. Two common ones:

```sh
# oras: copy a package out of a registry into a layout directory, or into a layout tar
oras cp --to-oci-layout registry.example.com/cargoship/my-package:1.0.0 ./my-package:1.0.0
oras backup --output my-package.tar registry.example.com/cargoship/my-package:1.0.0

# crane: --format=oci writes a layout directory (its default, tarball, does not work - see below)
crane pull --format=oci registry.example.com/cargoship/my-package:1.0.0 ./my-package
```

## Why There Is No `docker-archive` Support

`cargoship import` reads an OCI Image Layout only. A Docker image archive, the format `docker save` and `crane pull --format=tarball` write, cannot carry a Cargoship package, for two independent reasons:

- **No tool can produce one.** A Cargoship package is an OCI artifact with a custom config media type (`application/vnd.cargoship.config.v1+json`). Skopeo refuses to write it as a `docker-archive`, failing with `unsupported image-specific operation on artifact with type "application/vnd.cargoship.config.v1+json"`; `oras` has no `docker-archive` output at all; and the Docker daemon cannot pull an artifact with a non-image config, so `docker save` never sees one.
- **The format cannot carry what the import needs.** A `docker-archive`'s `manifest.json` records only `Config`, `RepoTags`, and `Layers`. There is no field for OCI annotations, so `org.opencontainers.image.title` - which is how a package layer declares whether it is `distro.yaml`, `checksums.txt`, or an image blob - is dropped. Even a hand-built `docker-archive` would arrive with its layers unidentifiable.

Use `oci-archive` or an OCI layout directory instead. Every tool above can write one.

## Verifying Signatures

If the package was signed before export, pass `--key` and `--verify` to validate its Cosign signature as part of the import:

```sh
cargoship import my-package.tar --verify=always --key ./cosign.pub
```
