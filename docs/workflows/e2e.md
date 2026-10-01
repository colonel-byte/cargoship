# e2e.yaml

**Triggers:** pull requests, merge groups.

Three jobs, "Basic build":

1. **build-e2e** - builds the `cargoship` binary (`CGO_ENABLED=0`, fully vendored/offline build) and uploads it as an artifact.
2. **e2e-noncluster** (needs build-e2e) - downloads that binary and runs the non-cluster e2e test groups (misc and package commands), which need no Docker/bootloose infrastructure. Uses `-short` to additionally skip example packages, which would otherwise pull ~1.5GB of engine artifacts and images.
3. **binary-size** (needs build-e2e) - compares the PR's binary size against the latest release's binary and posts the diff as a sticky PR comment. Pull requests only; there is no pull request to comment on in a merge group.

The cluster-dependent e2e group is deliberately not run here - it needs Docker, provisions ten containers, and installs RKE2 on nine of them, so it lives in its own workflow, `e2e-cluster.yaml`, on its own trigger.

`build-e2e` and `e2e-noncluster` are required status checks, which is why the `merge_group` trigger is here and why neither trigger carries a path filter. A path-filtered required check reports nothing at all on a change that only touches the ignored paths, so the check stays pending and the pull request can never be queued. The workflow used to skip `CODEOWNERS`-only changes; it now builds those too, which costs one build in the rare case and removes the stall.
