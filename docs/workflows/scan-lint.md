# scan-lint.yaml

**Triggers:** pull requests, merge groups.

Two independent lint jobs:

- **lint** - runs `golangci-lint` over the Go source.
- **containers** - runs `hadolint` against each container Dockerfile individually (`ansible`, `base`, `deb`, `ubi`), matrix-style. Paths are listed explicitly rather than using hadolint's recursive glob mode, which would otherwise pull in vendored Dockerfiles under `vendor/`.
