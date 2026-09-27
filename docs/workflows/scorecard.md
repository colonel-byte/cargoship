# scorecard.yaml

**Triggers:** branch protection rule changes, weekly schedule (Saturdays 01:30 UTC), push to `main`.

Runs the OpenSSF Scorecard supply-chain security analysis (`ossf/scorecard-action`) against the default branch, uploads the SARIF results as a workflow artifact, and publishes them to GitHub's code-scanning dashboard. Adapted from zarf-dev/zarf's equivalent workflow. Default permissions are `read-all`, with `security-events: write` and `id-token: write` granted only to the analysis job (for code-scanning upload and Scorecard's OIDC badge).
