# fix-vendor-osv.yaml

**Triggers:** `pull_request_target` (opened/reopened/synchronize) touching `vendor/**`, dependabot PRs only.

Dependabot re-runs `go mod vendor` on every dependency bump, which deletes and recreates `vendor/` - taking the four generated `osv-scanner.toml` override files with it (see `check-go-mod.yaml`). This workflow regenerates those files from the **base branch** (via `mage dev:writeOSVOverrides`) and commits them back onto the dependabot branch through the git data API.

Uses `pull_request_target` deliberately so the job runs against the base branch with a write-capable token, without ever checking out the PR's own tree - avoiding the classic `pull_request_target` vulnerability of running a write-privileged job against untrusted code. Overrides are generated purely from `magefiles/vendor-osv.go`, which no dependency bump touches, so the base branch produces the exact bytes the check wants.

Only runs for `dependabot[bot]`-authored PRs, since those branches live in this repo and can be pushed to with an app token; forks can't be pushed to, and nothing else re-vendors on a schedule.
