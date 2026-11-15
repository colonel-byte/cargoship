### Is your feature request related to a problem? Please describe.

`src/pkg/oci` can fetch a layer by descriptor (`FetchLayer`) and can fetch-and-unmarshal a layer by its file path (`FetchJSONFile`). The step in between - fetch the bytes of the layer at a known path and hand them back unparsed - is not exported. It is inlined into the typed helper, so any consumer whose parse is not a plain unmarshal has to write it out again.

The constraint is `FetchUnmarshal`'s unmarshaler parameter, `func(data []byte, v any) error`. A parse that takes a `context.Context`, or that returns a value rather than filling a pointer, cannot be expressed as one.

**Zarf's own code hits this.** [`zoci.FetchZarfYAML`](https://github.com/zarf-dev/zarf/blob/1abf721e0e06014fbc117ea0be0e3a6172924ced/src/pkg/zoci/fetch.go#L20) does not call `oci.FetchJSONFile`, and cannot - `pkgcfg.ParseMultiDoc` is `func(context.Context, []byte) (api.Package, error)`. So the locate-and-fetch is written out by hand:

```go
descriptor := root.Locate(layout.ZarfYAML)
if oci.IsEmptyDescriptor(descriptor) {
	return api.Package{}, fmt.Errorf("unable to find %s in the manifest", layout.ZarfYAML)
}
b, err := content.FetchAll(ctx, fetcher, descriptor)
```

That is the most important file in a zarf package - it carries a schema version and needs migrating - and it is the one the typed helper cannot serve.

### Describe the behavior you'd like

- **Given** a published package manifest and the path of a file in it
- **When** a caller wants the raw bytes to parse however that project parses things
- **Then** `src/pkg/oci` exports the step, and the typed helpers are defined in terms of it

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

`FetchJSONFile` becomes `FetchFile` followed by `json.Unmarshal`; `zoci.FetchZarfYAML` becomes `FetchFile` followed by `pkgcfg.ParseMultiDoc`. The name reads correctly beside `FetchLayer(ctx, desc)` - the same fetch, keyed by path rather than by descriptor.

This is a pure extraction: no existing signature changes, no behaviour changes, nothing deprecated, and the error text stays identical so no test asserting on it moves.

### Describe alternatives you've considered

**Widening `FetchUnmarshal`'s unmarshaler signature** to accept a context - this would break every current caller for the benefit of one shape of parser, where the extraction breaks none.

**Restoring `FetchYAMLFile[T]`**, which `github.com/defenseunicorns/pkg/oci` v1.3.2 had and the in-tree copy dropped. Deliberately not asked for here: a plain `goyaml.Unmarshal` is not how either project parses a package manifest, so it would sit unused beside the same hand-rolled four lines. If it is worth restoring it is worth its own issue.

### Additional context

Prototyped against v0.88.0 and reverted - 34 lines in `src/pkg/oci/fetch.go`, with `FetchJSONFile` redefined in terms of `FetchFile`, which is what shows the extraction is behaviour-preserving rather than merely additive. `FetchUnmarshal` untouched.

Possible intersection with the granular-layer work: [#5421](https://github.com/zarf-dev/zarf/pull/5421) landed the granular SBOM and documentation layers, and [#5524](https://github.com/zarf-dev/zarf/issues/5524) / [#5538](https://github.com/zarf-dev/zarf/pull/5538) are building retrieval by key on top of it. That is the same primitive from the other end - reading one known file out of a published artifact - and the retrieval path in #5538 still locates descriptors by hand rather than through a shared helper. Happy to shape `FetchFile` around whatever that work needs, or to hold it until #5538 lands so it can be extracted from the result rather than guessed at.

Happy to open the PR.
