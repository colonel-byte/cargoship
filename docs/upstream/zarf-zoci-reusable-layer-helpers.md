# Request: make `zoci`'s format-agnostic layer helpers reachable from outside zarf

- **Upstream**: [`zarf-dev/zarf`](https://github.com/zarf-dev/zarf)
- **Packages**: `src/pkg/zoci`, `src/pkg/images`, `src/internal/pkgcfg`
- **Read against**: v0.88.0, the release vendored here
- **Status**: open - not yet filed
- **Currency check**: `main` @ `2904d9e`, 2026-10-23 - still open. `src/pkg/zoci/fetch.go` still imports `src/internal/pkgcfg`, and `IsManifest`/`IsIndex` are still in `src/pkg/images`, whose `pull.go` still imports `docker/cli`, `moby/moby/client` and go-containerregistry's `daemon`/`layout`
- **Verified**: no - and cannot be locally. `src/pkg/zoci` and `src/pkg/images` are not vendored here, because cargoship imports neither, so there is nothing in this tree to patch and measure. The other requests in this folder were prototyped against the vendored `src/pkg/oci`; this one rests on reading upstream source and import graphs, and should be treated as the weaker evidence of the set

## What is awkward today

`src/pkg/zoci` contains the one piece of OCI logic that is genuinely hard to get right and is not specific to zarf's package format: walking a published manifest to collect the layer descriptors a given set of images needs, recursing through an image index into its per-platform children, and deduplicating the result. In v0.88.0 that logic is already factored to be reusable - `AssembleLayers`, `LayersFromImages` and `FetchZarfYAML` take `(root *oci.Manifest, fetcher content.Fetcher)` rather than a `*Remote`, so they work against any `oras` fetcher - and the recursion itself lives in the unexported `layersFromManifestChildren` and `layersFromIndexChildren` (`src/pkg/zoci/pull.go:207,222`).

None of it is reachable from another module, for three separate reasons. They are worth separating because they are very different sizes, and only the first two are small.

### 1. Two six-line media-type predicates live in a package that links the Docker CLI

`LayersFromImages` calls `images.IsIndex` and `images.IsManifest`. Those are media-type switches with no dependencies of their own (`src/pkg/images/common.go:78-93`):

```go
func IsManifest(mediaType string) bool {
	switch mediaType {
	case ocispec.MediaTypeImageManifest, DockerMediaTypeManifest:
		return true
	}
	return false
}
```

But Go links whole packages, and `src/pkg/images/pull.go` imports `github.com/docker/cli/cli/command`, `github.com/docker/cli/cli/context/docker`, `github.com/docker/cli/cli/flags`, `github.com/moby/moby/client` and `go-containerregistry`'s `daemon` and `layout` packages. So a consumer that wants to ask "is this media type an index?" links the Docker CLI and the Moby client to find out.

**Ask**: move the media-type predicates and their `DockerMediaType*` constants into a leaf package - `src/pkg/images/mediatype`, or alongside the descriptor helpers already in `src/pkg/oci/utils.go`. This is a file move plus re-exports for compatibility, and it benefits anything that needs the predicates without the pull path. [`zarf-oci-fetch-file-by-path.md`](zarf-oci-fetch-file-by-path.md) asks for the same class of thing in `src/pkg/oci` - a small primitive that consumers need, currently reachable only through something heavier - and is the cheaper of the two to accept.

### 2. `FetchZarfYAML` reaches into `src/internal/pkgcfg`

`src/pkg/zoci/fetch.go:13` imports `github.com/zarf-dev/zarf/src/internal/pkgcfg` and calls `pkgcfg.ParseMultiDoc` (`fetch.go:29`). Go's internal-package rule means **any external module importing `src/pkg/zoci` fails to compile**, whichever symbol it wanted. A package under `src/pkg/` that cannot be imported is effectively internal, and nothing in its name or docs says so.

We are not asking for `pkgcfg` to be exported wholesale - it imports `src/internal/api/v1alpha1` itself, so de-internalizing it cascades further than is reasonable for one caller. The narrow fix is better: `FetchZarfYAML` is the only function in the whole of `zoci` that touches `pkgcfg`, so either move it to a package that may legitimately depend on internals, or expose the parse behind a small public entry point in `src/api` (which already holds `api.Package` and already depends on the same conversion machinery).

**Ask**: make `src/pkg/zoci` compile from outside the module. Either fix is fine; the current state means `src/pkg/` makes a promise it does not keep.

### 3. The remaining signatures speak zarf's package format

`AssembleLayers` takes `[]api.Component` and reads `layout.*` filename constants; `PushPackage` takes `*layout.PackageLayout`. That is correct and we are not asking for it to change - a function that assembles the layers of a zarf package should say so in its types. It does mean the layer-walking recursion is only reusable by someone who already speaks zarf's format, and the recursion is the part with no zarf in it at all.

**Ask (optional, larger)**: export the index/manifest recursion on its own, taking a root descriptor, a fetcher and a predicate for which references to keep - no `api.Component`, no `layout` constants. If that is more refactoring than it is worth, items 1 and 2 are still worth doing independently.

**Caveat, added after upstream feedback.** This is the one ask on this page that is generic OCI work rather than zarf-package work, and upstream's stated bar is whether a change provides utility to people interacting with zarf packages and workflows - with the explicit note that something wanted for generic OCI things zarf itself would not use is better isolated from zarf than absorbed into it. That is a fair reading of this item and we are not pressing it. Items 1 and 2 do not depend on it: both are about zarf's own surface matching what it can be used for, and both stand if this one is declined.

## Why this is in upstream's interest

Items 1 and 2 are both cases where a package's public surface does not match what it can actually be used for, and both are cheap. Item 1 in particular means zarf's own binary links the Docker CLI anywhere the media-type predicates are needed, which may not be intended either.

Zarf has done this kind of untangling before and on request: [#3887](https://github.com/zarf-dev/zarf/issues/3887) refactored `src/internal/packager2/layout` specifically to avoid cyclic imports and duplication with `zoci`, [#2709](https://github.com/zarf-dev/zarf/issues/2709) untangled package imports, and [#4913](https://github.com/zarf-dev/zarf/issues/4913) exposed `LoadChartData` and `TemplateChart` as public API because a consumer asked.

## Evidence that it is needed

Cargoship (`colonel-byte/cargoship`) carries `pkg/coci`, a derived copy of `zoci`, and the reasons are recorded in [`docs/agent/choice-coci-fork.md`](../agent/choice-coci-fork.md). It is not a disagreement about the logic - the attribution headers are on every file and the layer-walking code is recognisably zarf's.

It is that the package cannot be imported (item 2), and that even if it could, the import closure is disqualifying for us: cargoship is carried onto air-gapped networks by hand and holds its binary to a size budget, and linking `zoci` pulls `docker/cli`, `moby/moby/client`, `go-containerregistry`, `sigstore/cosign` and `k8s.io`/`sigs.k8s.io` - through `src/pkg/images`, `src/pkg/packager/layout` and `src/pkg/utils` respectively - none of which is reachable from an air-gapped management node.

So we are being straightforward about this: **items 1 and 2 would not on their own let us delete `pkg/coci`.** We would still carry a fork, because the weight is the larger problem and because cargoship's package format genuinely differs from zarf's. What they would let us do is stop copying the two media-type predicates and stop having `src/pkg/zoci` look importable in a way it is not - and they would help any consumer whose format is closer to zarf's than ours is.

We do already import `src/pkg/oci`, which is clean: its entire closure is `oras-go`, `opencontainers/image-spec`, `src/pkg/helpers` and the standard library. That package is the model the rest of this is measured against.
