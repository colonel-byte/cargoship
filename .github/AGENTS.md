# Writing a pull request description

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

## Description

Open with one or two sentences carrying the core of the change: what is wrong or missing, and how the change answers it. Where the problem was found — a failing test, a fuzz corpus entry, a report — belongs in that opening, so a reviewer knows the change is not speculative.

Then add at most three `###` subsections, one per point a reviewer has to know about, each one or two sentences. Aim for 50 to 75 words of prose in a subsection, measured without any code block it holds; fewer is fine when the point is small, and a subsection past 100 words needs a reason to be that long. Spend them on what a reviewer cannot get from the diff: why the old behaviour was wrong, what the new behaviour is, or which behaviour that looks broken is in fact preserved. A change with one point of note gets one subsection; nothing obliges a description to reach three.

Tests and leftover work compete for the same three subsections. Add `### Tests` where the tests are themselves a point of note, saying what they pin down and which checks were run — and do not claim a check passed without running it. Add `### Follow-up` where the change leaves work behind for another branch or repository, saying what and where.

Name symbols, files, and identifiers in backticks — `EncryptAtPath`, `docs/dev/fuzz-tests.md`, `{0, pass: hunter2}` — so a reviewer can search for them.

Markdown in a pull request body follows the same rule as the rest of the repository: no hard-wrapped lines, one paragraph or list item per line. See [`docs/AGENTS.md`](../docs/AGENTS.md).

## Related Issue

Keep exactly one of the two forms. Use `Fixes #<n>` when merging the pull request should close the issue, and `Relates to #<n>` when it should not. Delete the `<!-- or -->` comment and the line you are not using. When no issue exists, write `Relates to N/A` rather than dropping the heading.

## Checklist before merging

Leave the box unchecked until it is true. Check it once tests, docs, or an ADR have been added or updated as the change needed — or once you have confirmed none were needed.

## Example

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
