# commitlint.yaml

**Triggers:** pull requests (milestoned, opened, reopened, synchronize, edited).

Lints the PR **title** - not individual commit messages - against conventional-commit rules, since release-please squash-merges PRs and the PR title becomes the final commit message on `main`. A dedicated lint action was written rather than reusing a commit-message-linting GitHub Action, because those lint the commits, not the title.

The `milestoned` event type is included as a workaround: release-please doesn't otherwise trigger pull_request workflows, so its PRs are added to a milestone specifically to fire this check.
