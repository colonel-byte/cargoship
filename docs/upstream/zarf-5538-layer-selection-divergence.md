# zarf `#5538` and `pkg/coci`'s layer selection

- **Upstream**: [`zarf-dev/zarf#5538`](https://github.com/zarf-dev/zarf/pull/5538), `feat(zoci)!: support granular layers from Load api` - open, targets [#5524](https://github.com/zarf-dev/zarf/issues/5524)
- **Read against**: `main` @ [`1abf721`](https://github.com/zarf-dev/zarf/commit/1abf721e0e06014fbc117ea0be0e3a6172924ced), 2026-10-28, and the PR's diff
- **Cargoship surface**: `pkg/coci/pull.go`, `pkg/coci/layers/`, `pkg/distro/pull.go`, `pkg/distro/import.go`

## It does not break the build

Said plainly, because the opposite was claimed in conversation first: **`#5538` cannot break cargoship's compile.** Cargoship does not import `src/pkg/zoci` - it carries a derived copy in `pkg/coci`, and `src/pkg/zoci` is not vendored - so nothing upstream renames or deletes reaches this tree. There is no bump that fails.

What `#5538` does is diverge from the design cargoship inherited, in the one area where cargoship's copy turns out not to work. The reason to file this is the second half, not the first.

## What `#5538` changes upstream

`zoci.AssembleLayers` loses its variadic layer-type parameter and gains a struct:

```go
// before
func AssembleLayers(ctx context.Context, root *oci.Manifest, fetcher content.Fetcher, requestedComponents []api.Component, include ...LayerType) ([]ocispec.Descriptor, error)

// after
func AssembleLayers(ctx context.Context, root *oci.Manifest, fetcher content.Fetcher, requestedComponents []api.Component, selection LayerSelection) ([]ocispec.Descriptor, error)
```

```go
type LayerSelection struct {
	Components  bool
	Images      bool
	SBOMs       *SBOMSelection
	Attachments *AttachmentSelection
}
```

`LayerType`, `GetAllLayerTypes`, `MetadataLayers`, `ComponentLayers`, `ImageLayers`, `SbomLayers` and `DocLayers` are deleted. The nil-versus-set pointers distinguish "do not fetch this kind at all" from "fetch this kind, filtered to these keys", which is what makes `zarf package inspect sbom --keys <image>` pull one SBOM instead of the whole tarball. "Documentation" is also renamed to "attachments" throughout.

## What cargoship has

`pkg/coci/layers/common.go` carries the same mechanism with cargoship's own membership - `MetadataLayers`, `ImageLayers`, `OSLayers`, `FileLayers` - and `pkg/coci/pull.go:96` keeps the variadic signature.

## The finding: the selection does nothing

Chasing the divergence turned up that cargoship's half of it is inert. `AssembleLayers` accepts `include ...layers.LayerType` and **never reads it**:

```go
func AssembleLayers(ctx context.Context, root *oci.Manifest, fetcher content.Fetcher, include ...layers.LayerType) ([]ocispec.Descriptor, error) {
	if len(include) == 0 {
		include = layers.GetAllLayerTypes() //nolint:ineffassign,staticcheck
	}

	// Metadata layers are always included
	layers := make([]ocispec.Descriptor, 0)
```

`include` is assigned on line 98 and never used again. Line 102 then declares a local named `layers`, shadowing the package of the same name for the rest of the function, so even a later read would not compile against the package. Every call returns every layer the package contains.

Four things follow, and they reinforce each other:

| Where                                                | What is there                                                                         | Why it hides the defect                                                                                                    |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `pkg/coci/pull.go:98`                                | `//nolint:ineffassign,staticcheck`                                                    | Suppresses exactly the two linters that flag an assigned-and-never-read value                                              |
| `pkg/coci/pull_test.go:205`                          | `TestAssembleLayersMetadataOnly` calls `AssembleLayers` with **no** include arguments | The name claims selection coverage the body does not exercise; it passes because the fixture only contains metadata layers |
| `pkg/distro/pull.go:125`                             | `PullOptions.LayerTypes []layers.LayerType`                                           | Set by nobody, read by nobody                                                                                              |
| `pkg/distro/pull.go:159`, `pkg/distro/import.go:128` | Both callers pass `layers.GetAllLayerTypes()...`                                      | Identical in effect to passing nothing, so no caller would ever notice                                                     |

So `LayerType`, `GetAllLayerTypes` and the four constants exist to be ignored, a `nolint` keeps the linter quiet about it, and the one test named for the behaviour does not pass the argument that would exercise it. Nothing is wrong at runtime today - everything wants every layer - but the API promises a filter it does not apply, and a future caller asking for metadata only would silently pull the whole package.

## Why now

The divergence and the defect have the same fix, which is the only reason to treat them as one piece of work. Upstream has just rebuilt this exact API into a shape that works, with a struct that distinguishes "not at all" from "filtered", and has done it in the open on `#5538`. Adopting that shape fixes cargoship's dead parameter rather than reimplementing a working version of the thing already copied.

The alternative - delete the parameter, the `layers` package and the dead field, and let `AssembleLayers` honestly return everything - is smaller and equally correct today. It gives up selective pull, which nothing currently asks for.

## What this is not

Not an upstream request. `#5538` is upstream solving an upstream problem, and the attribution headers in `pkg/coci` already record where the code came from. Nothing here needs filing against `zarf-dev/zarf`.

## Verification

Read from the working tree at the current `HEAD` and from `#5538`'s diff. The claims above are each a grep:

```sh
sed -n '96,142p' pkg/coci/pull.go                 # include never read after line 98
grep -rn "LayerTypes" --include=*.go . | grep -v vendor   # the dead field, two mentions total
sed -n '205,220p' pkg/coci/pull_test.go           # the test passes no include arguments
```

Not verified: whether removing the `//nolint` and fixing the shadowing makes `golangci-lint` flag anything further in that function, and whether any e2e path depends on `AssembleLayers` returning everything in a case the unit tests do not cover.
