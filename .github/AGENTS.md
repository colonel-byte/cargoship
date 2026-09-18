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

Open with a sentence or two naming what is wrong or missing and how the change answers it. Say where the problem was found — a failing test, a fuzz corpus entry, a report — so a reviewer knows the change is not speculative.

Then give one `###` subsection per distinct defect or feature in the change, and a final `### Tests` subsection. Inside each subsection, write bullets that explain the mechanism rather than restating the diff: what the code did, why that was wrong, what it does now, and which behaviour that a reviewer might expect to have broken is in fact preserved. A reviewer should be able to follow the reasoning without opening the files.

Name symbols, files, and identifiers in backticks — `EncryptAtPath`, `docs/dev/fuzz-tests.md`, `{0, pass: hunter2}` — so a reviewer can search for them.

`### Tests` lists the tests added, each with the case it pins down, and states which checks were run and that they pass. Do not claim a check passed without running it.

If the change leaves work behind for another branch or another repository, add a short `### Follow-up` subsection saying what still needs doing and where.

Markdown in a pull request body follows the same rule as the rest of the repository: no hard-wrapped lines, one paragraph or list item per line. See [`docs/AGENTS.md`](../docs/AGENTS.md).

## Related Issue

Keep exactly one of the two forms. Use `Fixes #<n>` when merging the pull request should close the issue, and `Relates to #<n>` when it should not. Delete the `<!-- or -->` comment and the line you are not using. When no issue exists, write `Relates to N/A` rather than dropping the heading.

## Checklist before merging

Leave the box unchecked until it is true. Check it once tests, docs, or an ADR have been added or updated as the change needed — or once you have confirmed none were needed.

## Example

[#422](https://github.com/colonel-byte/cargoship/pull/422), titled `fix(clustercfg): stop corrupting flow mappings and CRLF documents`, abridged:

```markdown
## Description

Two defects in the vault path splice, both found by the expanded fuzz suite on `tests/fuzzing`. Each one rewrites a cluster configuration into a file that no longer parses, and the first one does it with the credential already encrypted and the plaintext gone.

### Flow mappings holding a bare entry

- `EncryptAtPath` wrote a literal block scalar into a flow mapping such as `{0, pass: hunter2}`. YAML has no block scalar in flow style, so the result no longer parses -- and by then the plaintext is gone.
- `inFlowCollection` is meant to catch exactly this. It walked backwards along `node.GetToken().Prev` counting flow delimiters, but an entry written without a value makes the parser synthesise an `ImplicitNull` token whose `Prev` is nil. The walk stopped there and never reached the opening brace, so the check reported block style.
- The delimiters are now counted from `lexer.Tokenize(src)`, which holds only the tokens the lexer actually produced. `inFlowCollection` takes `src` as a result.
- The cases the old walk got right still are: `{z}` and `[1, 2]` on an earlier key read as block style, and a brace inside a quoted scalar is still part of that token rather than a delimiter of its own.

### Documents saved with CRLF

- go-yaml reports the wrong line for a value in a CRLF document: it counts the carriage return ending a comment as a line break of its own, so every comment above a value shifts that value's reported line down by one. In `inventory-paths.yaml` the credential on line 21 is reported on line 30, and the splice refuses the rewrite with `does not match the parsed document`.
- `onLineFeeds` converts a document written entirely in CRLF to line feeds, rewrites it, and converts it back. The conversion is an exact inverse: nothing this package writes holds a carriage return, since `renderScalar` quotes any value carrying one and both ciphertext formats are printable ASCII.
- A document with mixed endings is left alone, since there is no single ending to put back. For those, `lineEndOffset` now stops before the carriage return and `lineEndingAt` gives the splice the ending the value's own line already has, instead of a bare line feed.

### Tests

- `TestEncryptAtPathQuotesIntoAFlowMappingHoldingABareEntry` -- four flow shapes, each asserted to parse after the rewrite and to read the credential back.
- `TestPathsKeepCRLFLineEndings` -- a single-line credential and a literal block, each round-tripped through a CRLF fixture with no bare line feeds left behind and the document returned byte for byte.
- The exact fuzz corpus input, `{0,pass:00}`, encrypts, parses, and reads back.
- `go test ./...`, `go vet ./...` and golangci-lint are clean.

### Follow-up on the fuzzing branch

`docs/dev/fuzz-tests.md` still lists the flow crasher as open, and still says the CRLF shape knob should come back "once the splice carries the document's own line ending". Both need updating when this merges.

## Related Issue

Relates to N/A

## Checklist before merging

- [x] Test, docs, adr added or updated as needed
```
