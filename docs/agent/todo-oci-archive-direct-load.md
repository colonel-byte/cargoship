# TODO: Direct loading of Skopeo oci-archive in `apply`, `prepare`, and `load`

This document records the plan and considerations for supporting direct ingestion of Skopeo `oci-archive` (and OCI layout directories) in commands like `cargoship apply`, `cargoship prepare`, and `cargoship pull`/`load`.

## Problem Statement

When operating in an air-gapped network, artifacts are often moved across security boundaries via removable media using tools like Skopeo:
```sh
skopeo copy docker://registry.example.com/org/package:v1.0.0 oci-archive:package.tar:v1.0.0
```
While `cargoship import` converts this `oci-archive` file into a native Cargoship package (`cargoship-*.tar.zst`), users may want to pass `package.tar` directly to `cargoship apply package.tar --cluster cluster.yaml` without an explicit intermediate step.

However, an `oci-archive` can carry multiple manifests or image indices within a single archive (e.g. multi-arch builds or archives created by appending several images). Cargoship needs a deterministic way to select the desired package.

## Resolution Plan

### 1. Identifying the Source

In `pkg/utils/common.go:IdentifySource`:
- When given a `.tar` or a directory that contains an `oci-layout` and `index.json`, distinguish between a native Cargoship uncompressed package (`distro.yaml` at root) and an OCI Image Layout (`oci-layout` at root).
- Support explicit prefixes if necessary, e.g. `oci-archive:PATH[:TAG_OR_DIGEST]`.

### 2. Disambiguation / Package Selection

When an `oci-archive` contains multiple manifests in `index.json`:
- **Single manifest/index**: If `index.json` contains exactly one root entry (or one index that points to package manifests), auto-select it.
- **Reference/Tag matching**: Allow specifying the tag or digest, e.g. `oci-archive:package.tar:v1.0.0` or via `--tag` / `--reference`.
- **Target Architecture**: Match the platform/architecture requested by `--arch` or the host architecture against the descriptors in `index.json`.
- **Ambiguity Error**: If multiple distinct packages match without a clear tag or architecture selector, abort and list the available tags/digests in the archive (similar to how `pullOCI` / `constructIndexError` reports candidates), directing the user to disambiguate or use `cargoship import`.

### 3. Execution Pipeline

When detected in `pkg/distro/load.go`:
- Unpack/mount the OCI layout into a temporary directory or read directly using ORAS `oci.NewFromTar`.
- Resolve the target manifest descriptor.
- Extract layer blobs annotated with `org.opencontainers.image.title` into a temporary directory layout.
- Delegate to `layout.LoadFromDir` so lifecycle phases consume the resulting `DistroLayout` transparently.
