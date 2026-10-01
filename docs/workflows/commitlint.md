# commitlint.yaml

**Triggers:** pull requests (milestoned, opened, reopened, synchronize, edited), merge groups.

Lints the PR **title** - not individual commit messages - against conventional-commit rules, since release-please squash-merges PRs and the PR title becomes the final commit message on `main`. A dedicated lint action was written rather than reusing a commit-message-linting GitHub Action, because those lint the commits, not the title.

The `milestoned` event type is included as a workaround: release-please doesn't otherwise trigger pull_request workflows, so its PRs are added to a milestone specifically to fire this check.

`title_check` is a required status check, so the workflow also runs on `merge_group` to report one. A merge group carries no pull request and therefore no title, so every step is skipped there and the job reports success - the title was already linted on the pull request that got queued.
