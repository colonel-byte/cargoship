# prune-example-pr-packages.yaml

**Triggers:** daily schedule (05:23 UTC), manual dispatch (with `retention_days` and `dry_run` inputs).

Deletes throwaway GHCR container packages that `publish-example.yaml` writes when it runs on a pull request (published under `ghcr.io/colonel-byte/cargoship-examples/pr-N`, so PR test runs never overwrite the release reference). Nothing consumes these once the PR is done, and each one costs gigabytes of registry storage.

Only `pr-N` namespaces are ever touched - the release reference (`cargoship-examples/<flavor>`) is never a candidate. Candidates are enumerated from PRs carrying the `publish-example` label (the label that gates `publish-example.yaml` on PRs at all), then probed individually and deleted whole (not version-by-version - GHCR refuses to delete a package's last version, and whole-package delete also removes orphaned untagged child manifests) if their newest version is older than the retention window (default 30 days).

Package names are constructed from data already in the repo rather than listed via the GitHub API, because `GITHUB_TOKEN` can delete an individual package but can't list an organization's packages (`GET /orgs/{org}/packages` rejects Actions tokens).
