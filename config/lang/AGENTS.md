# Command strings in this package

## Do not write `<placeholder>` in any string here

Every string in this package is copied verbatim into `docs/commands/` by `mage generate:document`, and those files are chapters of the mdbook. A `Long` description becomes an ordinary markdown paragraph, so `<path>` in one is parsed as an HTML tag that is never closed:

```
WARN unclosed HTML tag `<path>` found in `commands/cargoship_schema.md` while exiting Paragraph
```

The chapter still renders, but everything from the tag to the end of the paragraph can be swallowed, and the warning stays in every build until someone chases it back to this package.

Name a placeholder in capitals instead, which is the convention Cobra already uses for positional arguments in the same strings:

```go
// Yes.
CmdSchemaLong = "... Point an editor at a written file with a '# yaml-language-server: $schema=PATH' comment on the first line."
CmdInstallLabelNodes = "Whether to check and add the node-role.kubernetes.io/PROFILE label on cluster nodes. ..."
```

```go
// No.
CmdSchemaLong = "... Point an editor at a written file with a '# yaml-language-server: $schema=<path>' comment on the first line."
CmdInstallLabelNodes = "Whether to check and add the node-role.kubernetes.io/<profile> label on cluster nodes. ..."
```

The rule covers every string in the package, not only the ones that currently render as prose. Where a string ends up in the generated chapter is a detail of how Cobra formats that command -- a flag description sits inside a fenced block today and is safe there, but a string moved from a flag to a description, or a chapter template that stops fencing, turns a safe angle bracket into a build warning with nothing in the diff to explain it. Capitals are safe everywhere.

Run `CARGOSHIP_MAGE_DOCS_CHILD=1 CARGOSHIP_CONFIG=hack/config.yaml mage generate:document` after editing a string here -- the pre-commit hook does it too -- and check that `mdbook build` reports no new warning.
