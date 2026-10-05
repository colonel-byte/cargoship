# dependabot-validate.yaml

**Triggers:** pull requests touching `.github/dependabot.yaml` or this workflow file.

Validates `.github/dependabot.yaml` using `marocchino/validate-dependabot`, then posts the validation result as a sticky PR comment. Catches malformed Dependabot config before it merges and silently breaks dependency update PRs.
