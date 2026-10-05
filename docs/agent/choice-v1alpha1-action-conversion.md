# Why onCreate actions go through zarf's package converter

`runCreateActions` in [`pkg/packager/assemble/actions.go`](../golang/pkg/packager/assemble.md) hands each action to zarf, and since zarf v0.87.0 that means converting it first. The conversion is one function, `toAPIActionSet`, and it does something that looks wasteful: it wraps a single action set in a throwaway `ZarfPackage` carrying one unnamed component, runs `convert.PackageFromV1alpha1` over the whole thing, and reads the converted action set back out of `Components[0]`. This document records why that is the shape, and the behavior change it brought with it, because both look like accidents from the code alone.

## What forced it

zarf v0.87.0 moved its canonical action model out of `src/api/v1alpha1` and into a normalized `src/api`, and retyped the executor against it:

```go
// v0.86.0
func Run(ctx context.Context, basePath string, defaultCfg v1alpha1.ZarfComponentActionDefaults, actions []v1alpha1.ZarfComponentAction, variableConfig *variables.VariableConfig, values value.Values, stateAccess template.StateAccess) error

// v0.87.0
func Run(ctx context.Context, basePath string, actions []api.Action, opts RunOptions) error
```

The v1alpha1 types still exist, so nothing in cargoship stopped compiling for lack of a type. What broke is the handoff: cargoship holds v1alpha1 actions and `Run` now takes `api.Action`.

cargoship holds v1alpha1 actions because its own package schema embeds them. `ZarfDistroActions.OnCreate` in [`api/zarf.dev/v1alpha1/distro/spec.go`](../golang/api/zarf.dev/v1alpha1/distro.md) is a `zarf.ZarfComponentActionSet`, which is what puts `mute`, `maxRetries`, `setVariable` and the rest into `schema/zarf-v1alpha1-distro-package-schema.json`. Retyping that field onto `api.Action` would convert the problem into a schema change -- `mute` becomes `silent`, `maxRetries` becomes `retries`, `wait.cluster.condition` stops being a string -- and every `distro.yaml` an operator has written would need rewriting. A dependency bump is not where that happens.

So the conversion has to live at the call site, and it has to be the same conversion zarf performs on a package it loaded itself. Anything else means cargoship's actions behave differently from zarf's for the same YAML.

## Why a whole package, for one action set

zarf's action converters are in `src/internal/api/v1alpha1`. `actionSetToGeneric`, `actionToGeneric` and `waitToGeneric` are exactly what is needed here and none of them are reachable: Go's internal-package rule is enforced by the compiler, not by convention. The only exported door through that boundary is [`src/api/convert`](https://github.com/zarf-dev/zarf/blob/main/src/api/convert/convert.go), and it converts packages.

The synthetic package is therefore the price of using zarf's mapping instead of writing a second copy of it. The alternative -- a hand-rolled `v1alpha1` to `api` converter in cargoship -- was rejected on how it fails rather than on how it reads. A hand-rolled converter compiles forever. When upstream adds an action field, or changes what an empty wait condition defaults to, cargoship keeps converting the old way and the action quietly runs with the old meaning. Going through `convert` fails at compile time if the function is removed or retyped, and tracks upstream silently when it is not.

## The field mapping is not a rename list

Three of the mappings would be easy to get wrong by hand, which is the concrete argument for not doing it by hand:

| v1alpha1 field            | `api` field                | Not a rename because                                                                                           |
| ------------------------- | -------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `mute`                    | `Silent`                   | The sense is preserved but the word inverts in `ActionDefaults`, where `Mute: true` became `Silent: true`      |
| `template`                | `EnableTemplating`         | `*bool` with a false default collapses to `bool`, through `ShouldTemplate`'s opt-in semantics                  |
| `wait.cluster.condition`  | `Condition.Expression`     | A string became a struct, and the converter stamps a `Default` of `existence` that the executor never reads    |

`TestToAPIActionSetMapsEveryField` pins all of them. A field that stops crossing is not a build failure -- the action still runs, just without whatever stopped crossing -- so the test is the only thing standing between a dropped `mute` and a package build that starts printing a command's output.

## What else runs, because the door is a package-level one

`convert.PackageFromV1alpha1` calls `migrateDeprecated` before it converts anything. That is the side effect of entering through the package door, and it is load-bearing in one case and inert in the rest.

**Inert:** `migrateScriptsToActions` reads `component.DeprecatedScripts`. The synthetic component has none and cargoship's schema has no scripts field at all, so it cannot fire.

**Load-bearing:** `migrateSetVariableToSetVariables` rewrites an action's deprecated singular `setVariable` into the `setVariables` list, when the list is empty. Under v0.86 nothing on this path read that field: `runAction` looked only at `SetVariables`. The field is in cargoship's schema, marked `deprecated` rather than rejected, so a `distro.yaml` could carry it and validate.

What that cost, measured against `test/e2e/noncluster/testdata/actions/distro.yaml` on the commit before the bump:

```text
ERR failed to create distro package: unable to run component before action: unable to render cmd of the "read both variables back" action: template "echo '{{ .Variables.FROM_LIST }}' > before-list.txt\necho '{{ .Variables.FROM_DEPRECATED }}' > before-deprecated.txt\n" resolved a missing key to "<no value>"
```

Not a silently unset variable. zarf ignored the field, so the variable was never set, and cargoship's own renderer -- which errors on a missing key rather than rendering `<no value>` into a shell command -- failed the build on the next action that read it. The three routes a variable can take to a later action, before and after:

| Route                                | v0.86                                       | v0.87                |
| ------------------------------------ | ------------------------------------------- | -------------------- |
| `setVariables` list                  | works                                       | works                |
| `setVariable`, deprecated singular   | variable never set, build fails on the read | works                |
| A value out of the values file       | works                                       | works                |

So the bump turns a hard failure into a working build for a definition the schema already accepted. That is the right direction, and it is still a behavior change: a package that failed to build now builds, and the file an action writes changes from nothing to a value.

One thing is lost on the way in. `migrateDeprecated` returns the warnings it accumulated, including "please migrate to the list form of setVariables", and `convert.PackageFromV1alpha1` discards them. The migration is silent. The wrapper that logs them, `ApplyMigrations`, is in the same internal package as the converters, so there is no exported way to get the warning without reimplementing the migration to produce it.

## What a reversal has to account for

Replacing `toAPIActionSet` with a direct converter is the obvious cleanup, and it costs three things that are not visible at the call site:

- the field mapping stops tracking upstream, in the silent direction described above;
- the deprecated `setVariable` stops working again, failing builds that work today, unless the migration is reimplemented alongside the mapping;
- `TestToAPIActionSetMigratesTheDeprecatedSetVariable`, `TestRunCreateActionsHonorsTheDeprecatedSetVariable` and `TestCargoshipCreateRunsOnCreateActions` fail, which is intended -- they exist so that the second point cannot be reverted by accident.

`TestRunCreateActionsDoesNotRewriteItsInput` covers the other direction. `migrateComponentActions` rewrites the slice it is handed in place, so `toAPIActionSet` is called with a fresh one-element slice per action rather than with the caller's own list. Converting the whole set in one call would hand zarf's migration the list that is written back out to `distro.yaml`, and the package would ship carrying migrations its author never made.
