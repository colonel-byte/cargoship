# Request: export the locate-and-fetch-bytes step in `src/pkg/oci`

- **Upstream**: [`zarf-dev/zarf`](https://github.com/zarf-dev/zarf)
- **Package**: `src/pkg/oci`
- **Read against**: v0.88.0, the release vendored here
- **Status**: open - not yet filed
- **Currency check**: `main` @ `2904d9e`, 2026-10-23 - still open. `src/pkg/oci/fetch.go` exports `FetchJSONFile`, `FetchUnmarshal` and `FetchLayer` and no `FetchFile`, and `zoci.FetchZarfYAML` still writes the locate-and-fetch out by hand
- **Verified**: yes - prototyped against vendored v0.88.0, see *Verification* below

## What is awkward today

`src/pkg/oci` can fetch a layer by descriptor (`FetchLayer`) and can fetch-and-unmarshal a layer by its file path (`FetchJSONFile`, `src/pkg/oci/fetch.go:98`). What it cannot do is the step in between: **fetch the bytes of the layer at a known path and hand them back unparsed.**

That step is not missing because nobody needs it. It is missing because it is inlined into the typed helper, so every consumer whose parse is not a plain unmarshal has to write it out again. `FetchJSONFile` is:

```go
descriptor := manifest.Locate(path)
if IsEmptyDescriptor(descriptor) {
	return result, fmt.Errorf("unable to find %s in the manifest", path)
}
return FetchUnmarshal[T](ctx, fetcher, json.Unmarshal, descriptor)
```

The constraint is `FetchUnmarshal`'s unmarshaler parameter, `func(data []byte, v any) error` (`src/pkg/oci/fetch.go:107`). A parse that needs a `context.Context`, or that returns a value rather than filling a pointer, cannot be expressed as one - and the most important file in a package is parsed exactly that way, because it carries a schema version and needs migrating.

**Zarf's own code demonstrates this.** `zoci.FetchZarfYAML` (`src/pkg/zoci/fetch.go:20`) does not call `FetchJSONFile`, and cannot:

```go
descriptor := root.Locate(layout.ZarfYAML)
if oci.IsEmptyDescriptor(descriptor) {
	return api.Package{}, fmt.Errorf("unable to find %s in the manifest", layout.ZarfYAML)
}
b, err := content.FetchAll(ctx, fetcher, descriptor)
if err != nil {
	return api.Package{}, err
}
defined, err := pkgcfg.ParseMultiDoc(ctx, b)
```

`pkgcfg.ParseMultiDoc` is `func(context.Context, []byte) (api.Package, error)`. It takes a context and returns a value, so it does not fit the unmarshaler signature, and the locate-and-fetch is written out by hand instead.

Across zarf and one downstream consumer this is the same four lines in three places, with the same error string - `unable to find %s in the manifest` - in all three.

| Where                                | Parses with            | Uses the typed helper? |
| ------------------------------------ | ---------------------- | ---------------------- |
| `oci.FetchJSONFile` (`fetch.go:98`)  | `json.Unmarshal`       | n/a - it is the helper |
| `zoci.FetchZarfYAML` (`fetch.go:20`) | `pkgcfg.ParseMultiDoc` | No - cannot            |
| `coci.FetchDistroYAML` (cargoship)   | `cfg.ParseMultiDoc`    | No - cannot            |

`content.FetchAll` has exactly three call sites in zarf, and two of them are the first two rows above; the third is an unrelated by-descriptor walk in `src/pkg/images/common.go`. There is no existing wrapper for this under another name.

## Proposed change

Export the step, and define the typed helpers in terms of it:

```go
// FetchFile returns the bytes of the layer in manifest whose title annotation matches path.
func FetchFile(ctx context.Context, fetcher content.Fetcher, manifest *Manifest, path string) ([]byte, error) {
	descriptor := manifest.Locate(path)
	if IsEmptyDescriptor(descriptor) {
		return nil, fmt.Errorf("unable to find %s in the manifest", path)
	}
	return content.FetchAll(ctx, fetcher, descriptor)
}
```

`FetchJSONFile` becomes `FetchFile` followed by `json.Unmarshal`; `zoci.FetchZarfYAML` becomes `FetchFile` followed by `pkgcfg.ParseMultiDoc`. The name reads correctly beside the existing `FetchLayer(ctx, desc)` - the same fetch, keyed by path rather than by descriptor.

This is a pure extraction. No existing signature changes, no behaviour changes, nothing is deprecated, and the error text stays identical so no test asserting on it moves.

## Verification

Prototyped against the vendored copy of v0.88.0 and then reverted. The patch is 34 lines in `src/pkg/oci/fetch.go`: `FetchFile` added exactly as written above, and `FetchJSONFile` redefined in terms of it -

```go
func FetchJSONFile[T any](ctx context.Context, fetcher content.Fetcher, manifest *Manifest, path string) (result T, err error) {
	b, err := FetchFile(ctx, fetcher, manifest, path)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(b, &result); err != nil {
		return result, err
	}
	return result, nil
}
```

which is what shows the extraction is behaviour-preserving rather than merely additive. `FetchUnmarshal` is untouched and keeps its existing callers.

Cargoship's `FetchDistroYAML` then became:

```go
b, err := oci.FetchFile(ctx, fetcher, root, config.DistroYAML)
if err != nil {
	return distro.Distro{}, err
}
return cfg.ParseMultiDoc(ctx, b)
```

Nine lines to five, with the `Locate`, the empty-descriptor check and the duplicated error string all gone. `TestFetchDistroYAMLFunc` passed unchanged, both subtests - including "errors when distro.yaml is not in the manifest", which is the one asserting the behaviour that moved into `FetchFile`. `go build ./...`, `go vet`, the full `go test -short` suite and golangci-lint were clean.

This is the cheapest of the requests in this folder to accept and the one with the most immediate effect on consumer code.

## What this request deliberately does not ask for

**Restoring `FetchYAMLFile`.** The package this file came from, `github.com/defenseunicorns/pkg/oci` v1.3.2, had `FetchYAMLFile[T]` - seven lines, `FetchUnmarshal` with `goyaml.Unmarshal` - and zarf's in-tree copy dropped it. That is a real regression for anyone who was using it, but it would not help here and we do not want it bundled in: a plain `goyaml.Unmarshal` is not how either project parses its package manifest, so `FetchYAMLFile` would sit unused beside the same hand-rolled four lines. If it is worth restoring, it is worth its own issue.

## Why this is in upstream's interest

The evidence is upstream's own: `zoci.FetchZarfYAML` bypasses `oci.FetchJSONFile` and writes the fetch out longhand, which is a signal that the useful primitive is one level below where the API currently starts. Exporting it removes a duplicated block inside zarf before any consumer benefits.

A zarf maintainer raised the same primitive unprompted when this folder was shared, as something the in-flight SBOM and document work would likely want - reading one known file out of a published artifact is not specific to the package manifest, and `FetchJSONFile` only serves the subset whose parse is a plain unmarshal.

It is also the smallest of the requests in this folder by some margin - one function, four lines, no signature churn - and it unblocks the single most common thing a library consumer wants from an OCI package client: read one known file out of a published artifact and parse it however that project parses things.

## Evidence that it is needed

Cargoship (`colonel-byte/cargoship`) carries `FetchDistroYAML` in `pkg/coci/fetch.go:38`, which is the third row of the table above: `root.Locate(config.DistroYAML)`, the empty-descriptor check with the identical error string, `content.FetchAll`, then `cfg.ParseMultiDoc` - cargoship's own multi-document parser with schema migration, which has the same shape as zarf's and the same incompatibility with `FetchUnmarshal`.

With `FetchFile` exported, those four lines become one call, and the function reduces to the parse that is genuinely ours. We already import `src/pkg/oci` - see [`choice-coci-fork.md`](../agent/choice-coci-fork.md) - so this is a change we would pick up on the next bump rather than one we would need to fork for.
