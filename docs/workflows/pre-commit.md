# pre-commit.yaml

**Triggers:** pull requests, merge groups.

Runs the hooks defined in `.pre-commit-config.yaml` (via `prek-action`) against all files, so formatting, licensing, and sorting checks can't drift from what runs locally.

Three hooks are explicitly skipped in CI:
- `schema-generator`, `docs-generator` - `repo: local` hooks that shell out to `mage` and need the full dev toolchain.
- `golangci-lint` - already covered separately by `scan-lint.yaml`.
