# Working in `.github/`

## Checking a workflow against OpenSSF Scorecard

This repository publishes an [OpenSSF Scorecard](https://securityscorecards.dev/viewer/?uri=github.com/colonel-byte/cargoship), and [`workflows/scorecard.yaml`](workflows/scorecard.yaml) recomputes it on every push to `main`. A workflow change can move that score without anything failing in CI, so run the checks that read workflows before opening the pull request, and name the ones you ran in the description.

The published score only refreshes after a merge, so score the working tree instead. Prefer the release binary over `gcr.io/openssf/scorecard`, which is not reliably pullable.

```sh
ver=$(gh release view --repo ossf/scorecard --json tagName -q .tagName)
curl -fsSL "https://github.com/ossf/scorecard/releases/download/${ver}/scorecard_${ver#v}_linux_amd64.tar.gz" | tar xz scorecard
./scorecard --local . --checks=Token-Permissions,Dangerous-Workflow,Pinned-Dependencies --show-details
```

`--local` reads the working tree, so it scores a change that is not yet committed. For an honest before and after, take the baseline from a clean export — `git archive HEAD | tar -x -C "$(mktemp -d)"` — rather than by stashing, which silently reports the wrong thing once the branch has moved under you.

Read [the check reference](https://github.com/ossf/scorecard/blob/main/docs/checks.md) for what a check measures and GitHub's [Secure use reference](https://docs.github.com/en/actions/reference/security/secure-use) for the practices behind it. What follows is only the handful this repository has actually tripped.

### Grant a permission on the job, not at the top of the file

Declare `permissions: contents: read` at the top of every workflow and put each write on the job that needs it. Token-Permissions charges the full ten points for a top-level `contents`, `packages`, or `actions` set to `write`, so one of them anywhere in the file takes the check to zero; the same grant on a job costs nothing.

A job-level block **replaces** the top-level one rather than merging with it, so a job that gains a write has to restate every read it still uses.

```yaml
# Yes.
permissions:
  contents: read

jobs:
  publish:
    permissions:
      contents: read
      packages: write
```

```yaml
# No -- zeroes the check, and grants the write to every job in the file.
permissions:
  contents: read
  packages: write
```

A job that calls a reusable workflow sets the ceiling for it: permissions can be held or narrowed down the chain, never widened. Write the caller's grant and the called job's grant to match.

### Never take a checkout ref from the event payload

Under `workflow_run` or `pull_request_target` a workflow runs with secrets and a writable token, so Dangerous-Workflow zeroes any `actions/checkout` whose `ref:` contains `github.event.pull_request` or `github.event.workflow_run` — that ref is attacker-controlled on a fork pull request, and the checkout hands it the token.

Sequence the work with a reusable workflow and `needs:` instead. A called workflow already runs at its caller's ref, so the checkout needs no `ref:` at all, and `needs:` orders the jobs more tightly than waiting on a run to finish. [`workflows/release.yaml`](workflows/release.yaml) calling [`workflows/publish-example.yaml`](workflows/publish-example.yaml) is the worked example.

### Pin every action to a full commit SHA

A `uses:` names the forty-character commit and carries the tag in a trailing comment, which is what Pinned-Dependencies counts and the only form that pins immutably — a tag can be moved.

```yaml
- uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
```

Dependabot reads the comment to offer the bump, so keep it accurate when you move the SHA.

### Pass an untrusted value through `env`, not into `run:`

Interpolating `${{ github.event.* }}` straight into a `run:` block splices attacker-controlled text into the shell. Bind it to an `env:` key and reference the variable, quoted, so the value reaches the script as data.

```yaml
# Yes.
env:
  TITLE: ${{ github.event.pull_request.title }}
run: echo "$TITLE"
```

```yaml
# No.
run: echo "${{ github.event.pull_request.title }}"
```

## Writing a pull request description

Every pull request description follows [`pull_request_template.md`](pull_request_template.md). GitHub prefills it when the pull request is opened in the browser; when opening one with `gh pr create`, pass the body yourself and keep the same three headings, in the same order, spelled the same way:

```markdown
## Description

...

## Related Issue

Fixes #
<!-- or -->
Relates to #

## Checklist before merging

- [ ] Test, docs, adr added or updated as needed
```

### Description

Open with one or two sentences carrying the core of the change: what is wrong or missing, and how the change answers it. Where the problem was found — a failing test, a fuzz corpus entry, a report — belongs in that opening, so a reviewer knows the change is not speculative.

Then add at most three `###` subsections, one per point a reviewer has to know about, each one or two sentences. Aim for 50 to 75 words of prose in a subsection, measured without any code block it holds; fewer is fine when the point is small, and a subsection past 100 words needs a reason to be that long. Spend them on what a reviewer cannot get from the diff: why the old behaviour was wrong, what the new behaviour is, or which behaviour that looks broken is in fact preserved. A change with one point of note gets one subsection; nothing obliges a description to reach three.

Tests and leftover work compete for the same three subsections. Add `### Tests` where the tests are themselves a point of note, saying what they pin down and which checks were run — and do not claim a check passed without running it. Add `### Follow-up` where the change leaves work behind for another branch or repository, saying what and where.

Name symbols, files, and identifiers in backticks — `EncryptAtPath`, `docs/dev/fuzz-tests.md`, `{0, pass: hunter2}` — so a reviewer can search for them.

Markdown in a pull request body follows the same rule as the rest of the repository: no hard-wrapped lines, one paragraph or list item per line. See [`docs/AGENTS.md`](../docs/AGENTS.md).

### Related Issue

Keep exactly one of the two forms. Use `Fixes #<n>` when merging the pull request should close the issue, and `Relates to #<n>` when it should not. Delete the `<!-- or -->` comment and the line you are not using. When no issue exists, write `Relates to N/A` rather than dropping the heading.

### Checklist before merging

Leave the box unchecked until it is true. Check it once tests, docs, or an ADR have been added or updated as the change needed — or once you have confirmed none were needed.

### Example

[#422](https://github.com/colonel-byte/cargoship/pull/422), titled `fix(clustercfg): stop corrupting flow mappings and CRLF documents`:

```markdown
## Description

Two defects in the vault path splice, both found by the expanded fuzz suite on `tests/fuzzing`, rewrite a cluster configuration into a file that no longer parses — the first of them with the credential already encrypted and the plaintext gone.

### Flow mappings holding a bare entry

`inFlowCollection` walked `node.GetToken().Prev` to count flow delimiters, and an entry written without a value gives the parser an `ImplicitNull` token whose `Prev` is nil, so the walk never reached the opening brace and `EncryptAtPath` wrote a block scalar into `{0, pass: hunter2}`. The delimiters now come from `lexer.Tokenize(src)`, which holds only the tokens the lexer produced.

### Documents saved with CRLF

go-yaml counts the carriage return ending a comment as a line break of its own, so every comment above a value shifts that value's reported line down by one and the splice refuses the rewrite. `onLineFeeds` converts a document written entirely in CRLF to line feeds, rewrites it, and converts it back, which is an exact inverse because nothing this package writes holds a carriage return.

### Tests

`TestEncryptAtPathQuotesIntoAFlowMappingHoldingABareEntry` covers four flow shapes and `TestPathsKeepCRLFLineEndings` round-trips a CRLF fixture byte for byte, both alongside the fuzz corpus input `{0,pass:00}`. `go test ./...`, `go vet ./...` and golangci-lint are clean.

## Related Issue

Relates to N/A

## Checklist before merging

- [x] Test, docs, adr added or updated as needed
```
