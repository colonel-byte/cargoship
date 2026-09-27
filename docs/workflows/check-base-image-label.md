# check-base-image-label.yaml

**Triggers:** pull requests touching `containers/**/Dockerfile`, `.release-please-manifest.json`, or `CHANGELOG.md`.

Compares each Dockerfile's final `FROM` line against its `org.opencontainers.image.base.name` label. Dependabot bumps the `FROM` line automatically, but the label is written by hand, so the two can silently drift - leaving consumers reading a label that names an image that's no longer actually used.

The digest is stripped from the `FROM` reference before comparison (the label carries the human-readable tag, not the digest). A reference pinned only by digest with no tag left is compared against `:latest`.

Drift doesn't fail the build - it's reported as a sticky PR comment, since the fix belongs in the same PR that bumped the `FROM` line.
