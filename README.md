# Cargoship

Cargoship is a Go-based CLI for building, distributing, and applying offline Kubernetes distro packages.

It is designed for two main jobs:

- create an offline Kubernetes distro package
- bootstrap and manage a cluster from that package over SSH

Cargoship combines ideas from distro packaging tools and remote cluster lifecycle tools, with support for packaging files and OCI images, publishing packages to registries, and applying them to target hosts.

## What Cargoship does

Cargoship supports the full lifecycle of an offline distro package:

- create a distro package from a definition directory
- publish a package to an OCI registry
- pull a package from a registry or URL
- prepare hosts before installation
- apply a package to bootstrap or upgrade a cluster
- fetch cluster kubeconfig
- reset or uninstall a cluster

The project currently includes distro integrations for:

- **RKE2**
- **K3s**

## How it works

Cargoship is organized around two main concepts:

### 1. Offline distro packages

A distro package contains the artifacts needed to install a Kubernetes distribution in disconnected or controlled environments. That includes:

- distro metadata
- host files
- engine configuration
- OCI images
- checksums and package layout metadata

The package creation pipeline loads a distro definition, assembles package content, and writes an archive for later distribution.

### 2. Phase-based cluster operations

Cluster operations are modeled as ordered phases. High-level actions such as `apply`, `prepare`, `reset`, and `kube-config` are composed from reusable phases.

For example, `apply` includes steps such as:

- connect to hosts
- detect OS
- gather facts
- validate hosts
- prepare hosts
- upload files
- configure the engine
- initialize or upgrade nodes
- update kubeconfig
- release locks and disconnect

This phase model makes cluster operations structured, debuggable, and easier to extend.

## Configuration model

Cargoship uses typed YAML definitions for:

- cluster inventory/config
- distro package definitions
- distro runtime config

These schemas are generated from Go types and written into `schema/`, which makes YAML authoring easier in editors that support JSON schema references.

The docs include an inventory authoring guide and example schema usage.

## Documentation

Project docs are built with mdBook and live under `docs/`.

Generated docs include:

- CLI command reference
- phase documentation
- summary/navigation content

Docs are generated from real code so the command and phase documentation stay aligned with implementation.

## Build and development workflow

This repo uses Mage for task automation and Dagger for the main build pipeline.

### Mage responsibilities

Mage targets are used for:

- local builds
- Dagger builds
- end-to-end tests
- docs generation
- schema generation

### Dagger responsibilities

Dagger is used as the default build path for producing binaries across platforms.

### Generated outputs

Automation in this repo maintains:

- `build/cargoship_*`
- `docs/commands/*`
- `docs/phases/*`
- `docs/SUMMARY.md`
- `schema/*.json`

## Examples

The `example/` directory contains sample distro content for:

- `k3s`
- `rke2`

These examples are useful for understanding package structure and authoring your own distro definitions.

## Release and CI

The repository includes GitHub workflows for:

- dependency validation
- build/test checks
- e2e runs
- docs deployment
- release automation

Releases are driven by GoReleaser, with checksum and signing support.

## Design notes

A few notable implementation patterns in the repo:

- side-effect registration for distro and OS modules
- phase-oriented orchestration for cluster workflows
- generated docs and schemas from source code
- offline-first packaging centered around files, images, and checksums

## Inspiration

This project is clearly influenced by tools such as:

- [k0sproject/k0sctl](https://github.com/k0sproject/k0sctl)
- [zarf-dev/zarf](https://github.com/zarf-dev/zarf)

while focusing on offline distro packaging plus SSH-based cluster lifecycle management.
