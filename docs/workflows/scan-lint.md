# scan-lint.yaml

**Triggers:** pull requests, merge groups.

Two independent lint jobs:

- **lint** - runs `golangci-lint` over the Go source, at the version [`.pre-commit-config.yaml`](../../.pre-commit-config.yaml) pins for the hook of the same name. The action installs the latest release when it is given no version, which once left CI linting with a newer golangci-lint than any contributor ran locally, so a failure here reproduced nowhere. Reading the pinned rev keeps the two in step without adding a second pin -- dependabot bumps the one in the hook config, and does not update inputs inside a workflow's `with:` block.
- **containers** - runs `hadolint` against each container Dockerfile individually (`ansible`, `base`, `deb`, `ubi`), matrix-style. Paths are listed explicitly rather than using hadolint's recursive glob mode, which would otherwise pull in vendored Dockerfiles under `vendor/`.
