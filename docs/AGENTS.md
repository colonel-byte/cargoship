# Markdown line formatting

## Do not hard-wrap lines in Markdown documents

Markdown files should NOT have hard-wrapped or truncated lines. Instead, write full paragraphs and list items on single, continuous lines without manual line breaks, letting editors and renderers handle line wrapping dynamically.

- Write entire paragraphs or list items on a single line without manual line breaks / hard wraps.
- Let editors and rendering tools handle line wrapping dynamically.

## Pad table cells so the columns line up

Tables are written in their aligned form, so that the source reads as a table and a reviewer can see the columns without rendering the file. Pad every cell with spaces to the width of the widest cell in its column, keep one space of padding inside each pipe, and run the dashes of the delimiter row out to that same width. Every line of the table then has the same length.

Write this:

```markdown
| Path                                   | What it holds     |
| -------------------------------------- | ----------------- |
| `.spec.config.registries[N].auth.user` | Registry username |
```

Not this:

```markdown
| Path | What it holds |
| --- | --- |
| `.spec.config.registries[N].auth.user` | Registry username |
```

Adding a row that is wider than the column repads the whole table, which is expected — the alignment is part of the content, not a one-time cleanup.

## Do not hand-edit the generated pages

Six parts of the `docs/` tree are generated from the code and are overwritten wholesale on the next run. `docs/commands/`, `docs/phases/`, `docs/ansible/module_*.md`, and `docs/ansible/role_*.md` are deleted and recreated, so an edit made there does not survive; `docs/index.md` and `docs/security.md` are rewritten from files that live outside `docs/` because GitHub reads them there; and `docs/SUMMARY.md` is rewritten from the tree that run produced.

| Path                       | Generated from                                                                      |
| -------------------------- | ----------------------------------------------------------------------------------- |
| `docs/commands/`           | The Cobra command tree, rendered by `doc.GenMarkdownTreeCustom`                     |
| `docs/phases/`             | The cluster phase descriptors named in `phaseDocs()` in `magefiles/gen-docs.go`     |
| `docs/ansible/module_*.md` | The Ansible module action plugins parsed in `generateModuleDocs()` in `gen-docs.go` |
| `docs/ansible/role_*.md`   | The role `meta/argument_specs.yml` files, parsed in `generateRoleDocs()`            |
| `docs/index.md`            | `README.md`, with its relative links rebased from the repository root onto `docs/`  |
| `docs/security.md`         | `.github/SECURITY.md`, with its relative links rebased the same way                 |
| `docs/SUMMARY.md`          | The mdBook table of contents, compiled from the rest of the `docs/` tree            |

To change one of those pages, change what it is generated from — a command's `Short`/`Long`/flag help, a phase's title and explanation, or `phaseDocs()` for which phase pages exist — and then regenerate:

```sh
go run ./magefiles/core generate:document
```

Use that `go run` form rather than `mage generate:document`. It runs the same entry point and needs only the Go toolchain, so it works on a host that has no `mage` binary installed. See [dev/mage.md](dev/mage.md) for the rest of the targets and what else each one writes.

Commit the regenerated files with the change that caused them, so the tree matches the code.
