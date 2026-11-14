# What cargoship still uses from zarf's v1alpha1

zarf v1.0.1 deprecated its `v1alpha1` package model: the `src/api/v1alpha1` types, the whole of `src/pkg/variables`, the `convert.Package*V1alpha1` family, and two methods on `template.Objects`. Everything still exists and still works - the deprecation is a doc comment, not a removal - so the v0.89.0 to v1.0.1 bump compiled with three small changes, none of which touched the package model. What it did change is `golangci-lint`, which runs `staticcheck` with `checks: ["all"]`, and SA1019 reports every use of a deprecated symbol.

This document records which of cargoship's code sits on that surface, why it sits there, and what moving off it costs. It is a map, not a plan: no migration has been started, and the bump deliberately did not start one.

## The bump itself

`github.com/zarf-dev/zarf v0.89.0` to `v1.0.1` broke three things, all in `cmd/`:

| What broke                                      | Fix                                                                             |
| ----------------------------------------------- | ------------------------------------------------------------------------------- |
| `message.TableWithWriter` removed               | `message.Table` takes the writer now; same signature, renamed at two call sites |
| `zlang.CmdPackagePublishFlagConfirm` removed    | A constant in `config/lang/eng-core.go`                                         |
| `zlang.CmdPackagePublishFlagSigningKeyPassword` | A constant in `config/lang/eng-core.go`                                         |

The confirm string was rewritten rather than copied across. zarf's version promised to skip a signing-key password prompt, and cargoship has no such prompt - the same divergence `CmdPackageCreateFlagConfirm` already records in `config/lang/eng-core.go`.

Two other upstream changes are visible in the vendored diff and affect nothing here, because nothing on this side reads them: `state.Distro` was removed and `template.WithState` now renders `.StorageClass` as the empty string, and `src/pkg/transform/artifact.go` was deleted. The binary moved from 39.98 MiB to 40.00 MiB, both measured with `mage build:binary` in one session. `docker/go-metrics` left the module graph and `agnivade/levenshtein` joined it.

## The lint wall

`golangci-lint run` over the module reports **25** SA1019 findings in six files. CI runs the linter with no path filter (`.github/workflows/scan-lint.yaml`), so this is a red check rather than a warning somebody can ignore. The pre-commit hook does not catch it, because the hook passes the linter only the files a commit touches, and these sites are already committed.

| File                                           | Findings | What it uses                                                                                     |
| ---------------------------------------------- | -------- | ------------------------------------------------------------------------------------------------ |
| `pkg/packager/actions/actions.go`              | 8        | `variables.VariableConfig` throughout, `template.WithVariables`, `api.Action.SetVariables`        |
| `pkg/packager/assemble/actions.go`             | 8        | `variables.New`, `convert.PackageFromV1alpha1`, `v1alpha1.ZarfComponentAction`, `ShouldTemplate` |
| `pkg/packager/assemble/actions_test.go`        | 5        | `api.Action.SetVariables`, `ShouldTemplate`                                                      |
| `pkg/packager/actions/actions_test.go`         | 2        | `variables`, `variables.New`                                                                     |
| `pkg/zarfpackage/zarfpackage.go`               | 2        | `v1alpha1.ZarfPackage`, `convert.PackageFromV1alpha1`                                            |
| `api/colonel-byte.dev/v1alpha1/distro/spec.go` | 1        | the `v1alpha1` import, for `ZarfComponentActionSet`                                              |

Reproduce it with `golangci-lint run 2>&1 | grep SA1019`. Use the bare form rather than `golangci-lint run ./...` when counting: the two did not report the same set here, and the `./...` form missed `api/`.

## A naming collision, first

`api/colonel-byte.dev/v1alpha1/` is **cargoship's own API group**, and has nothing to do with zarf's `src/api/v1alpha1`. Cargoship's `v1alpha1` is the version of the `Distro` and `Cluster` kinds an operator writes in a `distro.yaml` or an inventory; zarf's is the version of a `zarf.yaml`. They are unrelated version numbers on unrelated schemas, and nothing upstream deprecating the latter says anything about the former. The one place the two meet is the single field described immediately below.

## The four places

### 1. The distro schema embeds a zarf action set

`Actions.OnCreate` in [`api/colonel-byte.dev/v1alpha1/distro/spec.go`](../../api/colonel-byte.dev/v1alpha1/distro/spec.go) is a `zarf.ZarfComponentActionSet`. That one field is what puts `mute`, `maxRetries`, `setVariable`, `setVariables` and the rest of the action vocabulary into `schema/distro-v1alpha1-schema.json`.

This is the load-bearing one, and it is the reason the next two exist. Retyping it onto the normalized `api.ActionSet` is a breaking schema change for every `distro.yaml` an operator has written: `mute` becomes `silent`, `maxRetries` becomes `retries`, `wait.cluster.condition` stops being a string and becomes a struct. [`docs/agent/choice-v1alpha1-action-conversion.md`](../agent/choice-v1alpha1-action-conversion.md) has the full mapping and why it is not a rename list.

### 2. `assemble` converts the action set at the call site

[`pkg/packager/assemble/actions.go`](../../pkg/packager/assemble/actions.go) holds v1alpha1 actions because of the field above, and `actions.Run` takes `api.Action`, so `toAPIActionSet` wraps a single action set in a throwaway `ZarfPackage` and runs `convert.PackageFromV1alpha1` over it. That looks wasteful and is deliberate: zarf's real converters are in `src/internal/api/v1alpha1` and unreachable from here, and going through the package door also runs zarf's migrations, which is what makes a deprecated singular `setVariable` work at all. The whole argument - including the three tests that exist so it cannot be reverted by accident - is in `choice-v1alpha1-action-conversion.md`. Read it before touching this function.

### 3. `actions` keeps a `VariableConfig` for variable state

[`pkg/packager/actions/actions.go`](../../pkg/packager/actions/actions.go) is cargoship's fork of zarf's action executor (see [`docs/agent/choice-actions-fork.md`](../agent/choice-actions-fork.md)). It is already on the normalized `api.Action`, so the deprecated surface here is narrower than the finding count suggests - it is the `variables` package, used for three distinct jobs:

- **variable state across an action set.** `newVariableConfig` builds one `VariableConfig` for the whole `onCreate` set, so a variable one action sets is readable by the next. zarf builds a fresh one per call, which loses it.
- **`###ZARF_VAR_x###` and `$VAR` substitution.** `templateString` walks `variableConfig.GetAllTemplates()` and does literal string replacement. This is the older substitution pass, and it runs on the fields Go templating does not cover - the shell, the directory, the environment, and the string fields of a wait action.
- **the `setVariables` branch.** `SetVariable` plus `CheckVariablePattern` on an action's trimmed stdout, carrying the `sensitive`, `autoIndent`, `type` and `pattern` fields with it.

### 4. `zarfpackage` reads an operator's `zarf.yaml`

[`pkg/zarfpackage/zarfpackage.go`](../../pkg/zarfpackage/zarfpackage.go) decodes a `zarf.yaml` in whichever apiVersion it declares, and an omitted apiVersion is zarf's legacy spelling of v1alpha1. This one is not a cargoship design decision at all: the file is written by somebody else, and dropping the branch means refusing to read packages zarf itself still reads. It stays until zarf stops accepting them.

## What a migration would actually cost

The replacement for `variables` is `value.Values` plus `api.Action.SetValues`, and cargoship **already handles both** - `parseAndSetValue` at `pkg/packager/actions/actions.go:150` is the `SetValues` branch, running immediately after the `SetVariables` one in the same closure. So the executor is half migrated already, and what remains is not a mechanical substitution:

| What `variables` does           | What `value.Values` gives | Gap                                                                              |
| ------------------------------- | ------------------------- | -------------------------------------------------------------------------------- |
| `setVariables` with a `pattern` | `setValues` with a `type` | No regex validation on the way in; `CheckVariablePattern` has no counterpart     |
| `sensitive`, `autoIndent`       | nothing                   | A sensitive variable's value would stop being redacted, and stop being reindented |
| `###ZARF_VAR_x###` substitution | Go templating only        | The older pass covers fields Go templating never sees, including a wait action's  |
| `GetConstants`                  | values merged into one map | Constants and variables stop being separate namespaces inside a template          |

So it is a behaviour change for any `distro.yaml` using `setVariables` with a `pattern` or `sensitive`, on top of the schema break in place 1. That is a feature with its own migration story for operators, which is why the bump left it alone.

Note also that `api.SetValue.Value` is itself marked deprecated upstream, so the normalized model is not a settled destination either - part of the v1 replacement is already on the way out.

## What to do in the meantime

The honest options, in the order a reader should consider them:

1. **Leave it, and let CI be red on this one check.** Not viable for long, and while it is red it hides the next real SA1019.
2. **`//nolint:staticcheck // SA1019: <reason>` at each site.** Local and honest. It costs 25 comments and it makes the surface greppable, which the current state does not. The cheapest thing that keeps CI green without pretending the debt is gone.
3. **Exclude SA1019 for these six paths in `.golangci.yml`.** Fewer lines than option 2 and strictly worse: a path exclusion also silences the *next* deprecation in those files, and those files are exactly where the next one will be.
4. **Migrate.** A real change, scoped as above, with a version bump on cargoship's own `Distro` kind and a documented path for operators. It belongs behind its own issue, not inside a dependency bump.

Whichever is picked, `grep -rn 'SA1019' --include='*.go' .` should find the sites afterwards. Nothing else in the tree records them.
