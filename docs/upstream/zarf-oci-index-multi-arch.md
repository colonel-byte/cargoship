# Request: let `oci.UpdateIndexWithDescriptor` describe more than one architecture

- **Upstream**: [`zarf-dev/zarf`](https://github.com/zarf-dev/zarf)
- **Package**: `src/pkg/oci`
- **Read against**: v0.88.0, the release vendored here
- **Status**: open - not yet filed
- **Currency check**: `main` @ `2904d9e`, 2026-10-23 - still open. All three asks stand - `UpdateIndexWithDescriptor` still takes a single `ocispec.Platform`, `fetchIndex` is still unexported, and a tag resolving to a plain manifest is still an error
- **Verified**: yes - prototyped against vendored v0.88.0, see *Verification* below

## What is awkward today

`UpdateIndexWithDescriptor` (`src/pkg/oci/index.go:23`) takes a single `ocispec.Platform` and writes one entry into the index:

```go
func UpdateIndexWithDescriptor(ctx context.Context, repo *registryremote.Repository, tag string, platform ocispec.Platform, manifest ocispec.Descriptor) (ocispec.Descriptor, error)
```

That is the right shape when one manifest describes one platform. It is the wrong shape for a package whose *single* manifest is valid for several architectures at once, because the only way to express that is to call the function once per architecture - and each call re-fetches the index, re-marshals it and pushes a new index blob, so an N-architecture package does N round trips and leaves N-1 orphaned index blobs in the registry.

Two smaller things make the function hard to reuse even for the single-platform case:

1. **`fetchIndex` rejects a tag that resolves to a plain manifest** (`src/pkg/oci/index.go:71-72`): `OCI reference %q is %s, not an image index`. A repository that was previously published by a version of the tool that tagged a bare manifest cannot be migrated to an index in place - the first `UpdateIndexWithDescriptor` against that tag fails rather than starting a new index. Only `errdef.ErrNotFound` is treated as "nothing there yet" (`index.go:26-33`).
2. **The index is pushed untagged** (`index.go:53`), and the descriptor is returned for the caller to tag. That is a reasonable split, but it means every caller reimplements the tagging step, and the function cannot be used on its own to do the obvious thing.

## Proposed change

Take a slice of platforms, and treat an existing non-index tag as an empty index rather than an error:

```go
func UpdateIndexWithDescriptor(ctx context.Context, repo *registryremote.Repository, tag string, platforms []ocispec.Platform, manifest ocispec.Descriptor) (ocispec.Descriptor, error)
```

The body is almost unchanged - the existing replace-or-append loop (`index.go:36-46`) moves inside a loop over `platforms`, matching on `Platform.Architecture` exactly as it does now. Entries for architectures not named in `platforms` are left alone, which is what makes publishing an `amd64` package and then an `arm64` one of the same version leave both resolvable under one tag.

If changing the signature is unwelcome, the same result works as an addition - keep `UpdateIndexWithDescriptor` as a one-line wrapper over a new `UpdateIndexWithDescriptors` taking the slice. That is source-compatible for every existing caller.

Separately, and independently useful: have `fetchIndex` return the empty index for a tag that resolves to a non-index media type, instead of erroring. The current behaviour makes the function unusable against any repository with publishing history that predates the index.

## A third thing, found while prototyping: the read side is unexported

`fetchIndex` (`src/pkg/oci/index.go:60`) is unexported, and it is the only way to read an index back out of a repository. Production code does not need it - `UpdateIndexWithDescriptor` reads internally - but a **test** does, to assert on what was written.

When cargoship's `updateIndexForArches` was reimplemented on top of the proposed `UpdateIndexWithDescriptors` (below), the production body collapsed to four lines, and then a 36-line copy of `fetchIndex` had to be kept anyway purely so the existing tests could still read the index. That is the whole saving handed back.

**Ask**: export it, or something like it - `FetchIndex(ctx, repo, tag) (ocispec.Index, error)`. It is already written; only the capital letter is missing. Without it every consumer that tests its publish path reimplements the read.

## Verification

All three asks on this page were prototyped against the vendored copy of v0.88.0 and exercised by cargoship's existing test suite, then reverted. The patch is 60 lines across `src/pkg/oci/index.go`.

`UpdateIndexWithDescriptor` was kept as a one-line wrapper delegating to a new `UpdateIndexWithDescriptors` taking `[]ocispec.Platform`, so the change is source-compatible; the replace-or-append loop moved inside a loop over platforms, and `fetchIndex` returns `errdef.ErrNotFound` for a non-index tag instead of an error, which the caller already handles by starting an empty index.

Cargoship's `updateIndexForArches`, `fetchIndex` and `pushIndex` were then replaced by:

```go
func (r *Remote) updateIndexForArches(ctx context.Context, tag string, publishedDesc ocispec.Descriptor, arches api.Arches) error {
	platforms := make([]ocispec.Platform, 0, len(arches))
	for _, arch := range arches {
		platforms = append(platforms, oci.PlatformForArch(string(arch)))
	}
	desc, err := oci.UpdateIndexWithDescriptors(ctx, r.Repo(), tag, platforms, publishedDesc)
	if err != nil {
		return err
	}
	return r.Repo().Tag(ctx, desc, tag)
}
```

The eight existing assertions in `TestUpdateIndexForArches` and `TestFetchIndexUntaggedVersion` passed **unchanged** against the upstream implementation - including the three subtests that pin the multi-architecture behaviour ("a package covering two architectures lists one manifest under both", "publishing a second architecture under the same tag keeps the first", "republishing an architecture replaces its entry") and the one that pins the non-index tolerance. `go build ./...`, `go vet`, the full `go test -short` suite and golangci-lint were all clean.

Net effect on cargoship: 53 lines deleted, 12 added. 36 of those 12 are the test-only index reader described above, so with a `FetchIndex` export the saving roughly doubles.

Two details for whoever writes the real patch. The new loop takes the address of the range variable (`desc.Platform = &platform`), which is only safe under Go 1.22+ per-iteration semantics - zarf's `go.mod` declares `go 1.26.9`, so it holds, but this is the shape of the classic aliasing bug and is worth a second look in review. And `fetchIndex`'s deferred close only overwrites `err` when `err == nil`, so returning `errdef.ErrNotFound` from the media-type branch is not clobbered by it.

## Why this is in upstream's interest

Zarf already has the multi-architecture case in its own domain - `PlatformForSkeleton` and `MultiOS = "multi"` both exist because a package is not always one architecture - so the single-platform signature is likely to become awkward upstream too, not only downstream. `src/pkg/oci/index_test.go` is already in place to extend, and the change is additive to its existing cases.

## Evidence that it is needed

Cargoship (`colonel-byte/cargoship`) builds fat packages: one tarball carries every architecture it targets, as one set of blobs and therefore one manifest, and the host's architecture selects what gets uploaded at apply time. It cannot use `UpdateIndexWithDescriptor` and carries `updateIndexForArches` in `pkg/coci/push.go` instead - the same replace-or-append logic, wrapped in a loop over the package's architectures, with its own `fetchIndex` that tolerates a tag left behind by an older release that tagged a manifest directly.

That function is about 30 lines and is the main reason cargoship's publish path cannot sit directly on `src/pkg/oci`. It has unit coverage (`TestUpdateIndexForArches`, `TestFetchIndexUntaggedVersion`) that would transfer upstream with it, and we are happy to open the PR.
