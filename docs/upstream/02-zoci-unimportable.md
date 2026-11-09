### Describe what should be investigated or refactored

`src/pkg/zoci` cannot be imported by any module outside `zarf-dev/zarf`. [`src/pkg/zoci/fetch.go:13`](https://github.com/zarf-dev/zarf/blob/1abf721e0e06014fbc117ea0be0e3a6172924ced/src/pkg/zoci/fetch.go#L13) imports `github.com/zarf-dev/zarf/src/internal/pkgcfg` and calls `pkgcfg.ParseMultiDoc`. Go's internal-package rule means an external module importing `src/pkg/zoci` fails to compile, whichever symbol it wanted.

A package under `src/pkg/` that cannot be imported is effectively internal, and nothing in its name or its docs says so. Since `src/pkg/` is the surface zarf offers library consumers, that is a promise the layout makes and the import graph does not keep.

There are two such imports, and both are recent:

| Internal package        | Reached from                                                                                                              | Used by                                                 |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- |
| `src/internal/pkgcfg`   | [`fetch.go:13`](https://github.com/zarf-dev/zarf/blob/1abf721e0e06014fbc117ea0be0e3a6172924ced/src/pkg/zoci/fetch.go#L13) | `FetchZarfYAML` only - `pkgcfg.ParseMultiDoc`           |
| `src/internal/checksum` | [`pull.go:236`](https://github.com/zarf-dev/zarf/blob/1abf721e0e06014fbc117ea0be0e3a6172924ced/src/pkg/zoci/pull.go#L236) | `checksumVerifiedSBOMLayers` - `GetSHA256Hash`, `Parse` |

`checksum` arrived with [#5421](https://github.com/zarf-dev/zarf/pull/5421) on 26 October, which is worth noting on its own: the internal surface inside `src/pkg/zoci` is growing rather than static, because nothing fails when it does. Each is reached from exactly one function, so neither is deeply entangled - but closing only one of them leaves the package just as unimportable.

### Links to any relevant code

- https://github.com/zarf-dev/zarf/blob/1abf721e0e06014fbc117ea0be0e3a6172924ced/src/pkg/zoci/fetch.go#L13 - the `internal/pkgcfg` import
- https://github.com/zarf-dev/zarf/blob/1abf721e0e06014fbc117ea0be0e3a6172924ced/src/pkg/zoci/fetch.go#L29 - the single `pkgcfg.ParseMultiDoc` call site
- https://github.com/zarf-dev/zarf/blob/1abf721e0e06014fbc117ea0be0e3a6172924ced/src/pkg/zoci/pull.go#L236-L243 - the `internal/checksum` call sites in `checksumVerifiedSBOMLayers`

### Additional context

Not asking for either package to be exported wholesale - `pkgcfg` imports `src/internal/api/v1alpha1` itself, so de-internalizing it cascades much further than one caller justifies. For the `pkgcfg` half, two narrower fixes:

1. Expose the parse behind a small public entry point in `src/api`, which already holds `api.Package`, already depends on the same conversion machinery, and already owns the API-version knowledge a multi-doc parse needs - `KnownAPIVersions()` is there today. The in-flight v1beta1 work ([#5551](https://github.com/zarf-dev/zarf/pull/5551), [#5453](https://github.com/zarf-dev/zarf/pull/5453)) is extending that package anyway, so this may be cheaper to do now than later.
2. Move `FetchZarfYAML` to a package that may legitimately depend on internals, leaving `zoci` importable.

`internal/checksum` looks like the easier half either way: `GetSHA256Hash` and `Parse` over a checksums manifest are not zarf-package-specific in the way a multi-doc parse is, so a leaf package under `src/pkg/` would carry them without the cascade.

The two `pkgcfg` options differ in a way that may matter for timing: option 1 is purely additive - `src/api` gains an entry point, `zoci.FetchZarfYAML` keeps its name and signature, and no existing caller moves. Option 2 removes a symbol from `src/pkg/zoci`, so it is a breaking change and would need to land before v1.0.0 or go through whatever deprecation policy follows it. If that distinction is the deciding factor, option 1 closes the gap just as completely.

Reading a published zarf package's `zarf.yaml` from another module is a thing consumers want to do, and `zoci.FetchZarfYAML` is exactly the function for it - it already takes `(root *oci.Manifest, fetcher content.Fetcher)` rather than a `*Remote`, so it is written to be reusable. The imports are the only thing stopping it.

Happy to open the PR for whichever shapes maintainers prefer, or to split it so the `checksum` half lands independently.
