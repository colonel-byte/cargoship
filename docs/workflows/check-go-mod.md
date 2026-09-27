# check-go-mod.yaml

**Triggers:** pull requests, merge groups.

Two checks that keep the module graph and its vendored security metadata honest:

1. Runs `go mod tidy` and fails if it produces a diff against the committed `go.mod`/`go.sum`.
2. Runs `mage dev:verifyVendor` to check the four generated `osv-scanner.toml` override files under `vendor/` are present and current. These overrides keep the Scorecard Vulnerabilities check reporting on cargoship's own code rather than on the JS/Python tooling its Go dependencies happen to ship in their own repos.

`go mod vendor` deletes and regenerates `vendor/`, so any PR that re-vendors (which Dependabot always does) drops these override files and fails this check on the first push. `fix-vendor-osv.yaml` regenerates and pushes them back onto Dependabot's branch so the check passes on rerun. A genuinely dropped module or newly uncovered lockfile still fails here, since fixing that needs a manual edit to `osvOverrides`.

Uses `go run ./magefiles/core` rather than an installed `mage` binary - same targets, but built from `vendor/` with no extra tool needed on the runner.
