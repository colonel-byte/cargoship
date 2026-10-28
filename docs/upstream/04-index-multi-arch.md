### Is your feature request related to a problem? Please describe.

[`oci.UpdateIndexWithDescriptor`](https://github.com/zarf-dev/zarf/blob/main/src/pkg/oci/index.go#L23) takes a single `ocispec.Platform` and writes one entry into the index:

```go
func UpdateIndexWithDescriptor(ctx context.Context, repo *registryremote.Repository, tag string, platform ocispec.Platform, manifest ocispec.Descriptor) (ocispec.Descriptor, error)
```

That is the right shape when one manifest describes one platform. It is the wrong shape when a *single* manifest is valid for several architectures at once, because the only way to express that is to call the function once per architecture - and each call re-fetches the index, re-marshals it and pushes a new index blob. An N-architecture package costs N round trips and leaves N-1 orphaned index blobs in the registry.

Two smaller things make the function hard to reuse even in the single-platform case:

1. **`fetchIndex` rejects a tag that resolves to a plain manifest** ([`index.go:71-72`](https://github.com/zarf-dev/zarf/blob/main/src/pkg/oci/index.go#L71-L72)): `OCI reference %q is %s, not an image index`. Only `errdef.ErrNotFound` is treated as "nothing there yet". So a repository published by an older version of a tool that tagged a bare manifest cannot be migrated to an index in place - the first `UpdateIndexWithDescriptor` against that tag fails rather than starting a new index.
2. **`fetchIndex` is unexported, and it is the only way to read an index back out of a repository.** Production code does not need it, but a *test* does, to assert on what was written. Every consumer that tests its publish path reimplements the read.

### Describe the behavior you'd like

- **Given** a package whose one manifest is valid for several architectures
- **When** it is published under a tag
- **Then** one index fetch, one index push, one entry per architecture

```go
func UpdateIndexWithDescriptors(ctx context.Context, repo *registryremote.Repository, tag string, platforms []ocispec.Platform, manifest ocispec.Descriptor) (ocispec.Descriptor, error)
```

The body is almost unchanged - the existing replace-or-append loop moves inside a loop over `platforms`, matching on `Platform.Architecture` exactly as it does now. Entries for architectures not named in `platforms` are left alone, which is what makes publishing an `amd64` package and then an `arm64` one of the same version leave both resolvable under one tag.

Keeping `UpdateIndexWithDescriptor` as a one-line wrapper over it is source-compatible for every existing caller.

Two independent, smaller asks in the same file:

- have `fetchIndex` treat a non-index media type as an empty index rather than an error (the caller already handles the empty case);
- export the read side - `FetchIndex(ctx, repo, tag) (ocispec.Index, error)`. It is already written; only the capital letter is missing.

### On zarf's own caller

The only non-test caller on `main` is [`src/pkg/component/publish.go:214`](https://github.com/zarf-dev/zarf/blob/1abf721e0e06014fbc117ea0be0e3a6172924ced/src/pkg/component/publish.go#L214), which publishes one architecture per call - so the multi-architecture inefficiency above is not currently zarf's problem, and this request should be read as "the signature forecloses a case" rather than "zarf is doing N round trips today".

That same function is worth flagging against the `fetchIndex` tolerance ask, because it cuts the other way. [#5386](https://github.com/zarf-dev/zarf/pull/5386) added explicit guards just above the call:

```go
if architecture == "" && err == nil && existing.MediaType == ocispec.MediaTypeImageIndex {
	return fmt.Errorf("cannot publish architecture-generic component: %s already contains architecture-specific variants", componentRef.String())
}
if architecture != "" && err == nil && existing.MediaType != ocispec.MediaTypeImageIndex {
	return fmt.Errorf("cannot publish architecture-specific component: %s already contains an architecture-generic artifact", componentRef.String())
}
```

So zarf treats a tag whose media type does not match the publish mode as an error *deliberately*, and does it at the caller with a better message than the library could give. The ask here is not to remove that. It is that the same decision is currently also hardwired into `fetchIndex` one layer down, where a consumer with different migration needs cannot opt out - and `component/publish.go` would keep its guards either way, because it resolves the tag itself before calling in. If maintainers would rather the library stay strict, that half can be dropped; the signature change and the `FetchIndex` export do not depend on it.

`component/publish.go` is also the place a `FetchIndex` export would pay off in-tree: it is the publish path whose result `src/pkg/oci/index_test.go` has to read back.

### Describe alternatives you've considered

**Changing the existing signature in place** rather than adding the plural form - cleaner, but breaks callers for no functional gain over the wrapper.

**Leaving the read side unexported and having consumers reimplement it** - which is the status quo, and measurably wasteful: when a consumer's publish path was rebuilt on the proposed API, the production body collapsed to four lines and then a 36-line copy of `fetchIndex` had to be kept anyway so the existing tests could read the index back. That handed the whole saving back.

### Additional context

Prototyped against v0.88.0 and reverted - 60 lines across `src/pkg/oci/index.go`. Eight existing assertions passed **unchanged** against the upstream implementation, including the three that pin the multi-architecture behaviour (one manifest listed under both architectures; publishing a second architecture keeps the first; republishing an architecture replaces its entry) and the one that pins the non-index tolerance.

Two details for whoever writes the real patch: the new loop takes the address of the range variable (`desc.Platform = &platform`), safe only under Go 1.22+ per-iteration semantics - `go.mod` declares `go 1.26.9`, so it holds, but it is the shape of the classic aliasing bug and worth a second look. And `fetchIndex`'s deferred close only overwrites `err` when `err == nil`, so returning `errdef.ErrNotFound` from the media-type branch is not clobbered.

On fit with the project: zarf already has the multi-architecture case in its own domain - `PlatformForSkeleton` and `MultiOS = "multi"` exist because a package is not always one architecture - so the single-platform signature looks likely to get awkward in zarf's own code, not only in a consumer's. `src/pkg/oci/index_test.go` is already in place and the change is additive to its existing cases. If maintainers read the fat-package shape as outside what zarf packages do, the `fetchIndex` tolerance and the `FetchIndex` export stand on their own and can be split out.

Happy to open the PR.
