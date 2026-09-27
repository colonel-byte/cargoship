# refresh-examples.yaml

**Triggers:** weekly schedule (Mondays 06:41 UTC), manual dispatch (with a `no_cache` input for when upstream re-cuts release assets in place).

Regenerates everything under `example/` from whatever upstream RKE2/k3s has released since the last run, and opens (or updates) a single pull request with the result. Nothing else generates `example/` on a schedule - the mage targets involved are otherwise run by hand - so this turns "staying current with upstream" into a review task instead of something someone has to remember.

Runs three mage targets in a fixed, dependency-ordered sequence: `generate:updatePins` (re-pulls source for any moved pin, writes `thirdparty-src/pins.json`) → `generate:engineConfig` (reads that source into each version's component vocabulary) → `generate:examples` (renders values schemas against that vocabulary). Running them out of order means a schema can render empty instead of failing.

After rendering, it runs `go build ./...` and the schema validation tests against the result and reports pass/fail in the PR body - a render that doesn't compile still opens the PR (so it's visible for debugging) but the job itself fails.

The branch (`chore/refresh-examples`) is force-pushed each week rather than accumulating one PR per run, so any hand edit to that branch is lost on the next refresh by design - template changes belong in `magefiles/templates/` instead. Uses a GitHub App token (same one release-please uses) rather than `GITHUB_TOKEN`, since a PR opened with the default token wouldn't trigger `pull_request` workflows like e2e or pre-commit.

Publishing is a separate concern - this workflow only writes definitions; `publish-example.yaml` picks them up at the next release or on demand.
