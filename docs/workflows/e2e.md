# e2e.yaml

**Triggers:** pull requests (excluding `CODEOWNERS`-only changes).

Three jobs, "Basic build":

1. **build-e2e** - builds the `cargoship` binary (`CGO_ENABLED=0`, fully vendored/offline build) and uploads it as an artifact.
2. **e2e-noncluster** (needs build-e2e) - downloads that binary and runs the non-cluster e2e test groups (misc and package commands), which need no Docker/bootloose infrastructure. Uses `-short` to additionally skip example packages, which would otherwise pull ~1.5GB of engine artifacts and images.
3. **binary-size** (needs build-e2e) - compares the PR's binary size against the latest release's binary and posts the diff as a sticky PR comment.

The cluster-dependent e2e group is deliberately not run here - it needs Docker, provisions ten containers, and installs RKE2 on nine of them, so it lives in its own workflow, `e2e-cluster.yaml`, on its own trigger.
