# release-please.yaml

**Triggers:** push to `main`, manual dispatch.

Runs `googleapis/release-please-action` to manage version bumps, `CHANGELOG.md`, and release PRs, driven by `release-please-config.json` and `.release-please-manifest.json`. It mints a GitHub App token up front and hands it to the release-please action for every write it makes - `GITHUB_TOKEN` itself is never granted write permission.
