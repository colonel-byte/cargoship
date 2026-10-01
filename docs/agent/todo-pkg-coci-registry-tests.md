# TODO: registry-backed unit tests for `pkg/coci`

This is a punch list, not a decision record like the `choice-*.md` files beside it. A coverage pass on `pkg/coci`, `pkg/oci/archive`, and `pkg/oci/platform` split the remaining gaps into three tiers. Tier 1 (pure helpers: `layers.GetAllLayerTypes`, `GetOCICacheModifier`, `CopyPackage`'s retry validation) and Tier 2 (the layer-assembly logic in `pull.go` and `fetch.go`'s free-function `FetchDistroYAML`, all fakeable with an in-memory `content.Fetcher` and a hand-built `*oci.Manifest` -- no registry involved) are both done; see `pkg/coci/common_test.go`, `pkg/coci/copier_test.go`, `pkg/coci/pull_test.go`, `pkg/coci/layers/common_test.go`. This document is the remainder: Tier 3, the functions that need a real (or in-memory) OCI registry to exercise meaningfully.

## What's left, and why it needs a registry

These all call through `*Remote` (an `*oci.OrasRemote` wrapper) to a `content.Fetcher`/`oras.Target` backed by an actual repository -- resolving tags, fetching roots, walking indexes against real registry HTTP semantics -- which the Tier 2 fake fetcher does not attempt to reproduce faithfully:

- `(*Remote).PullDistro` (`pull.go`) -- drives `oras.Copy`/`CopyToTarget` against a real target; needs a populated registry to pull from and a `file.Store` destination on disk.
- `(*Remote).AssembleLayers`, `(*Remote).FetchDistroYAML`, `(*Remote).FetchImagesIndex` (thin wrappers around the already-tested free functions) -- the wrapper logic itself (`FetchRoot` then delegate) is what's untested, not the delegated logic.
- `PushPackage` (`push.go`) -- pushes a `layout.DistroLayout` to a target and updates the index; this is the inverse of pull and needs the same setup.
- `CopyPackage`'s happy path (`copier.go`) -- only the negative-retries validation is covered today; the actual `oras.Copy` + `UpdateIndex` path needs two registries (source and destination).

## How to approach it

Follow the pattern already established in `pkg/coci/push_test.go` / `fetch_test.go`: `test.SetupInMemoryRegistry(t)` (documented in `docs/agent/choice-in-memory-oci-registry.md`) plus the local helpers `testRemote`, `pushManifest`/`pushManifestTagged`, `pushBlob`, `readIndex`. For `CopyPackage`, that means two `testRemote` instances against two registry addresses (or two repos on one in-memory registry, whichever `test.SetupInMemoryRegistry` supports without a rewrite -- check before assuming you need two servers).

For `PullDistro`/`PushPackage`, you'll need a real `layout.DistroLayout` -- reuse `writeChecksummedPackage` (`pkg/packager/layout/package_test.go`) or a similar minimal on-disk fixture rather than inventing a new one, since it already produces a package that passes `validateDistroIntegrity`.

## Scope

Don't expand this into re-covering Tier 1/2 logic through a registry "for realism" -- that's duplicated effort for the same reason `pkg/action`'s punch list (`todo-pkg-action-unit-tests.md`) says not to re-prove wiring the e2e suite already proves. The value here is specifically the registry-interaction code paths (auth, tagging, index read-modify-write against a real target, oras.Copy semantics) that the Tier 2 fakes cannot exercise.
