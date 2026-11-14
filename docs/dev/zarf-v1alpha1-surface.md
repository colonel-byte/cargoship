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

`golangci-lint run` over the module reports **30** SA1019 findings in six files. The `lint` job in [`.github/workflows/scan-lint.yaml`](../../.github/workflows/scan-lint.yaml) runs the linter with no path filter, so this is a red check rather than a warning somebody can ignore.

**Count them with the caps off**, or the number is wrong and unstable:

```sh
golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 2>&1 | grep -c SA1019
```

golangci-lint's `max-same-issues` defaults to 3, and it groups by message text - so each distinct deprecated symbol reports at most three times and the rest are dropped silently. A plain `golangci-lint run` reported exactly `25 issues` both locally and in CI while naming a *different* 25 each time, which reads as a stable number and is not one. The same applies to any future SA1019 sweep here: the truncated output is not a work list.

A local `prek run --all-files` will not reproduce the findings at all, for two separate reasons worth knowing apart. The `prek` job in [`.github/workflows/pre-commit.yaml`](../../.github/workflows/pre-commit.yaml) passes `--skip golangci-lint`, so the hook is not what gates this in CI - the `lint` job is. And locally the hook hands the linter only the files being committed, so sites already committed before the bump are never passed to it.

| File                                           | Findings | What it uses                                                                                     |
| ---------------------------------------------- | -------- | ------------------------------------------------------------------------------------------------ |
| `pkg/packager/actions/actions.go`              | 8        | `variables.VariableConfig` throughout, `template.WithVariables`, `api.Action.SetVariables`       |
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

| What `variables` does           | What `value.Values` gives  | Gap                                                                               |
| ------------------------------- | -------------------------- | --------------------------------------------------------------------------------- |
| `setVariables` with a `pattern` | `setValues` with a `type`  | No regex validation on the way in; `CheckVariablePattern` has no counterpart      |
| `sensitive`, `autoIndent`       | nothing                    | A sensitive variable's value would stop being redacted, and stop being reindented |
| `###ZARF_VAR_x###` substitution | Go templating only         | The older pass covers fields Go templating never sees, including a wait action's  |
| `GetConstants`                  | values merged into one map | Constants and variables stop being separate namespaces inside a template          |

So it is a behaviour change for any `distro.yaml` using `setVariables` with a `pattern` or `sensitive`, on top of the schema break in place 1. That is a feature with its own migration story for operators, which is why the bump left it alone.

Note also that `api.SetValue.Value` is itself marked deprecated upstream, so the normalized model is not a settled destination either - part of the v1 replacement is already on the way out.

## How it is suppressed, and why that way

Every site carries a per-line `//nolint:staticcheck // SA1019: v1alpha1 is load-bearing here; see docs/dev/zarf-v1alpha1-surface.md` - **28 directives** covering the 30 findings. `grep -rn 'SA1019' --include='*.go' .` is the index; nothing else in the tree records them.

A path exclusion for these six files in `.golangci.yaml` would have been three lines instead of 28, and was rejected on what it costs rather than on how it reads: an exclusion also silences the *next* deprecation in those files, and these are precisely the files the next one will land in. A per-line directive fails loudly when the line it sits on changes and is flagged by `nolintlint` when it stops suppressing anything, so it cannot outlive what it was written for.

Two mechanical facts about placing these, both of which cost a round trip here:

- **Only a trailing comment works.** A standalone `//nolint` line above a statement suppresses nothing in golangci-lint, and above a `for` block it does not reach the block's contents either. The directive has to be on the reported line.
- **A trailing comment inside a method chain fights gofmt.** `template.NewObjects(values).WithConstants(...).WithVariables(...)` spans three lines, and a trailing directive on each made gofmt reindent the continuations to seven tabs. Both chains - in `runAction` and in `renderAction` - are now plain statements reassigning an `objs` variable, which is what lets each line carry its own directive and stay formatted. The behaviour is identical: `WithConstants` and `WithVariables` are value-receiver methods returning `Objects`.

The suppression is not the end state. The migration described above is still the actual fix, and it belongs behind its own issue rather than inside a dependency bump.
