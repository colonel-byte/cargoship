# Why the API group moved, and why `Zarf` names remain afterwards

Cargoship's own configuration documents used to declare `apiVersion: zarf.dev/v1alpha1` with `kind: ZarfCluster` and `kind: ZarfDistro`, and the Go types behind them lived in `api/zarf.dev/v1alpha1/`. They now declare `colonel-byte.dev/v1alpha1` with `kind: Cluster` and `kind: Distro`, from `api/colonel-byte.dev/v1alpha1/`.

The move itself is not the interesting part. What this document records is the part that looks like an unfinished job: after a rename whose entire point was to stop saying `Zarf`, the tree still says `Zarf` in several places, and each one is deliberate.

## What forced the move

`zarf.dev` belongs to the upstream Zarf project, and upstream already defines kinds under it -- `ZarfPackageConfig` and `ZarfInitConfig` in [`vendor/github.com/zarf-dev/zarf/src/api/v1alpha1/package.go`](https://github.com/zarf-dev/zarf/blob/main/src/api/v1alpha1/package.go). A custom API group is supposed to be an FQDN the authoring project controls, for exactly the reason that two projects defining kinds in one group have no way to tell their documents apart. Cargoship was squatting in a live namespace, and nothing but luck kept upstream from one day shipping a `ZarfCluster` of its own.

The second reason is a layer boundary. Cargoship manages the host, the OS, and the Kubernetes engine lifecycle; Zarf packages applications on top of the cluster that results. Naming cargoship's root types `ZarfCluster` and `ZarfDistro` put the lower layer under the upper layer's name.

## What did not move, and why

### Upstream types keep their names

`ZarfComponentAction`, `ZarfComponentActionSet`, `ZarfComponentActionDefaults`, `ZarfComponentActionWait`, `ZarfPackage`, `ZarfChart`, `ZarfDataInjection` and the rest are upstream's types, referenced out of `vendor/`. Renaming them is not available -- they are not ours -- and aliasing them locally would buy a cosmetic win at the cost of a second name for one type.

### So the distro schema still has Zarf-named `$defs`

This is the visible consequence, and the one most likely to be mistaken for a missed spot. `schema/distro-v1alpha1-schema.json` contains:

```json
"$defs": {
  "ZarfComponentAction": { ... },
  "ZarfComponentActionSet": { ... },
  "ZarfComponentActionWaitCluster": { ... }
}
```

`$defs` keys are bare Go type names, and `Actions.OnCreate` is a `zarf.ZarfComponentActionSet` by deliberate choice -- see [choice-v1alpha1-action-conversion](choice-v1alpha1-action-conversion.md), which explains why that field is typed on upstream's model and why retyping it would be a breaking schema change for every `distro.yaml` an operator has written. Until that field changes type, those keys stay. Renaming the API group was never going to remove them.

### The deprecated group is still accepted

`v1alpha1.DeprecatedAPIVersion` is still a constant, still an entry in `internal/cfg`'s `knownAPIVersions`, still a key in `pkg/schema`'s `documentKinds`, and still an `enum=` on both roots. That is the transition window, not residue. Deleting any one of them breaks documents that parse today.

Two details worth knowing before touching them:

- **The dispatch table is priority-ordered.** `colonel-byte.dev/v1alpha1` sits at priority 2 and `zarf.dev/v1alpha1` at priority 1, so `ParseMultiDoc` on a package carrying one document per group reads the newer one. `TestHandlerFor` fails if two entries ever share a priority, because "latest" would stop being well defined.
- **The combinations are accepted independently.** A document may pair either group with either kind. Nothing cross-checks them, and `TestValidatorAcceptsTheDeprecatedSpelling` pins that on purpose: an operator half-way through editing a file should not be told the two halves disagree.

## What the rename did change, that a reader might not expect

**`$defs` keys and `$ref` targets moved.** They are bare Go type names, so dropping the `Zarf` prefix from the Go types rewrote both published schemas -- `#/$defs/ZarfHost` became `#/$defs/Host` -- without any document format changing. A rename of an unexported-looking Go type is a change to a published artefact here.

**One generator reads a `$defs` key by name.** `magefiles/pkg/gen/schema/schema.go` looks up `defObj["Host"]` to stamp `required: ["role"]` onto the host subschema. It is an `ok`-checked assertion, so if the key stops matching the type name it skips silently and the constraint disappears from the published schema with no error anywhere. Any future rename of `cluster.Host` has to change that string in the same commit.

**The rename went through `gopls`, not `sed`.** `ZarfFile` was declared by both this project and upstream, so the selector `v1alpha1.ZarfFile` meant different types in different files depending on which `v1alpha1` that file imported. Every non-vendor use happened to resolve to ours, so a textual rename would have survived -- by luck. A textual rename that retargets an upstream reference produces no compile error, because both types exist. Renaming ours to `File` removed the ambiguity for good, but the same hazard still applies to the near-miss pairs `BuildData`/`ZarfBuildData` and `Metadata`/`ZarfMetadata`.

## Not done, on purpose

**Annotation prefixes were not touched.** The original proposal called for migrating `zarf.dev/<key>` annotations to `colonel-byte.dev/<key>`. There are none to migrate. Every `zarf.dev/` label and annotation in the tree is upstream's -- `zarf.dev/agent`, `zarf.dev/package`, `zarf.dev/connect-name`, `zarf.dev/namespace-override` -- and all of them live in `vendor/`. Renaming them would break the contract with the Zarf agent rather than assert ownership of anything.

**The version stayed at `v1alpha1`.** The schema is still in flux, and a group move is not a reason to spend the version bump.

**Schema hosting did not move.** The `$schema` URLs still point at `raw.githubusercontent.com`. Only the filenames changed, so the 165 generated editor headers kept resolving through the whole migration.
