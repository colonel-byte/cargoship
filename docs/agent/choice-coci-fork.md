# Why `pkg/coci` is a fork of zarf's `zoci`, and why `pkg/oci` is not

Cargoship publishes and pulls distro packages over OCI. The client underneath that is zarf's - `pkg/coci` is a derived copy of `github.com/zarf-dev/zarf/src/pkg/zoci`, with the attribution headers on every file - and the package it sits on, `github.com/zarf-dev/zarf/src/pkg/oci`, is imported rather than copied. Those two different answers for two adjacent packages are the thing this document explains, because from inside `pkg/coci` the duplication looks like something to clean up and there is nothing in the code to say what cleaning it up would cost.

## `src/pkg/zoci` cannot be imported at all

This is the load-bearing fact, and it is invisible from here.

`src/pkg/zoci/fetch.go` imports `github.com/zarf-dev/zarf/src/internal/pkgcfg`, for one call to `pkgcfg.ParseMultiDoc`. Go's internal-package rule means **any module outside `github.com/zarf-dev/zarf/src/...` fails to compile if it imports `src/pkg/zoci`**, whichever symbol it wanted. Not "links something unwanted" - fails to build. A package under `src/pkg/` that cannot be imported is effectively internal, and neither its path nor its doc comment says so.

So for `zoci` the choice was never reuse-or-fork. It was fork or reimplement, and a derived copy that keeps upstream's attribution and tracks upstream's logic is the better of those two.

## What linking it would cost even if it compiled

Worth recording, because "they fixed the internal import" is going to look like the moment to delete this package, and it is not sufficient on its own. `zoci` reaches the following through three of its imports:

| Via                        | Pulls                                                                                                                      |
| -------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `src/pkg/images`           | `docker/cli/cli/command`, `docker/cli/cli/flags`, `moby/moby/client`, `go-containerregistry` (`crane`, `daemon`, `layout`) |
| `src/pkg/packager/layout`  | `sigstore/cosign/v3/pkg/cosign`                                                                                            |
| `src/pkg/utils`            | `k8s.io/api`, `k8s.io/apimachinery`, `sigs.k8s.io/yaml`, `go-containerregistry`, `cosign/pkg/oci/remote`                   |

That is the Docker CLI, the Moby client, cosign and the Kubernetes API machinery, none of it reachable from an air-gapped management node, arriving to support a package format cargoship does not write. Against the binary budget in [`AGENTS.md`](../../AGENTS.md#the-binary-has-a-size-budget) that is disqualifying on its own, independently of the internal import. No MiB figure is quoted here on purpose: the import closure is the argument, and a number measured against a build that cannot be produced would be fiction.

`src/pkg/images` is the sharpest case, and it is the one worth asking upstream about. `zoci` needs exactly two things from it - `IsIndex` and `IsManifest`, six-line media-type switches with no dependencies - and gets `docker/cli` and `moby/moby/client` with them, because Go links whole packages and `src/pkg/images/pull.go` imports those.

## `src/pkg/oci` is clean, and is imported

The package underneath `zoci` is the opposite case. Its entire import closure is `oras.land/oras-go/v2`, `github.com/opencontainers/image-spec`, `zarf/src/pkg/helpers` and the standard library - every one of which cargoship already linked. It carries no custom media types, no zarf annotation keys, no `zarf.yaml`, and no zarf struct types in any signature; it is a proper functional-options package with `Modifier` and six `With*` options covering transport, TLS, plain HTTP, user agent, logger and cache.

So it is imported. Cargoship was on `github.com/defenseunicorns/pkg/oci` until zarf dropped that dependency and brought the same code in-tree as `src/pkg/oci` at v0.88.0; staying behind would have left cargoship the sole significant consumer of a library its only other consumer had left. The swap was a one-line import change in ten files - every symbol and method cargoship uses is signature-identical, and `setRepository` differs by one slog key - and it dropped both `defenseunicorns/pkg/oci` and the indirect `defenseunicorns/pkg/helpers/v2` from `go.mod`, 2503 vendored lines.

**It was not a binary win and should not be reported as one**: 39.46 MiB to 39.44 MiB with `mage build:binary`, both built in the same session. Nothing new is linked and nothing stopped being linked, because the two packages are the same code. The win is one fewer module to track and one fewer orphaned dependency; see the warning about vendor size in [`AGENTS.md`](../../AGENTS.md#measuring-binary-size-means-building-the-way-a-release-does).

Two things in `src/pkg/oci` are inherited oddities rather than decisions, and both are written up in [`docs/upstream/`](../upstream/zarf-oci-registry-rewrite.md): `setRepository` rewrites the registry names `🦄` and `defenseunicorns` to `ghcr.io/defenseunicorns/packages/...`, and credentials are read from the Docker config file with no injection point. **Both are fixed on zarf's `main`** - the shorthand was deleted in [#5504](https://github.com/zarf-dev/zarf/pull/5504) and `WithCredentialFunc` added in [#5502](https://github.com/zarf-dev/zarf/pull/5502), both 2026-10-21 - but neither is in v0.88.0, so the vendored copy here still has both.

Cargoship tracks zarf's releases rather than `main`, so the following is a note for whenever the next bump happens rather than a reason to make one. Two further things in `src/pkg/oci` moved after v0.88.0. `oci.WithCache` now takes `content.Storage` rather than `*orasOCI.Store`, which `GetOCICacheModifier` still satisfies, so it is source-compatible for us. The change it came with is the one to read: zarf stopped building the image-layer cache as an `oci.Store` and now uses `oci.NewStorage`, because a `Store` maintains an index and a concurrent `oras.Copy` writing through it races on that index. Cargoship's `pkg/images/pull.go` still constructs the cache the old way and sets `copyOpts.Concurrency`, so the same exposure is here; `oci.NewStorage` is already in the vendored `oras-go`, so taking the fix does not wait on the bump.

## What diverges on purpose, and must survive a resync

Everything else in `pkg/coci` should stay recognisably zarf's. These four are cargoship's and are not upstream bugs:

1. **`updateIndexForArches` (`pkg/coci/push.go`)** replaces the `OrasRemote.UpdateIndex` it shadows. A cargoship package is fat - one set of blobs, therefore one manifest, valid for every architecture it targets - so the index lists that one manifest once per architecture. Upstream's `UpdateIndexWithDescriptor` takes a single platform and would need one call, one index fetch and one index push per architecture. Requested upstream in [`docs/upstream/zarf-oci-index-multi-arch.md`](../upstream/zarf-oci-index-multi-arch.md).
2. **`fetchIndex`'s tolerance of a non-index tag (`pkg/coci/push.go`)** returns an empty index for a tag that resolves to a plain manifest, which is what a cargoship old enough to have tagged one directly left behind. Upstream errors instead, so publishing over such a tag would fail rather than migrate it. Same request as above.
3. **`SoleManifestDigest` (`pkg/coci/fetch.go`)** falls back to the one manifest in an index when the host's architecture matches none of them.
4. **`layersFromIndexChildren` and `layersFromManifestChildren` (`pkg/coci/pull.go`)** walk a multi-arch index when collecting image layers to publish. See [`choice-image-index.md`](choice-image-index.md) for why a package stores several architectures under one index.

The cargoship-specific names are deliberate too: `CargoshipConfigMediaType`, `CargoshipLayerMediaTypeBlob`, `config.DistroYAML` in `PackageAlwaysPull`, and the `layers.LayerType` set, which describes cargoship's layout rather than zarf's components-and-sboms one.

## Do not revert this to an upstream import

There is no import to revert to. `src/pkg/zoci` does not compile from here, and the fix for that upstream would not be enough by itself - see the table above. If the internal import is ever resolved, the question to ask is still whether linking `src/pkg/images`, `src/pkg/packager/layout` and `src/pkg/utils` is acceptable, and the answer is still no while those pull the Docker CLI and the Kubernetes API machinery.

When resyncing `pkg/coci` with a newer zoci, re-apply exactly the four divergences listed above and take upstream's wording for everything else. The upstream asks that would narrow this fork are drafted in [`docs/upstream/`](../upstream/zarf-zoci-reusable-layer-helpers.md) and have not been filed.

## Known drift inside this package

Recorded here rather than fixed, so it is not rediscovered as a surprise:

- **`AssembleLayers`'s `include` parameter does nothing.** `pkg/coci/pull.go:97-98` assigns `include = layers.GetAllLayerTypes()` and never reads it, behind a `//nolint:ineffassign,staticcheck` that silences the linter which would have caught it. The doc comment above it promises filtering that does not happen, so `MetadataLayers`, `ImageLayers`, `OSLayers` and `FileLayers` are decorative. Both call sites pass `GetAllLayerTypes()`, so no caller is wrong today.
- **Three constants are defined twice.** `CargoshipConfigMediaType` and `CargoshipLayerMediaTypeBlob` (`pkg/coci/common.go`) and `OCITimestampFormat` (`pkg/coci/push.go`) duplicate identical values in `pkg/packager/layout/oci.go:51-56`. The writer and the reader of the same format can drift apart silently.
- **Stale zarf wording.** Comments in `pkg/coci/layers/common.go` still say "Zarf cache" and "zarf.yaml" for constants describing cargoship's layout.
- **Registry-backed paths have no unit coverage.** `PullDistro`, `PushPackage` and `(*Remote).AssembleLayers` are the standing Tier 3 list in [`todo-pkg-coci-registry-tests.md`](todo-pkg-coci-registry-tests.md).
