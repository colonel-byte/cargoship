### Describe what should be investigated or refactored

`images.IsManifest` and `images.IsIndex` are six-line media-type switches with no dependencies of their own:

```go
func IsManifest(mediaType string) bool {
	switch mediaType {
	case ocispec.MediaTypeImageManifest, DockerMediaTypeManifest:
		return true
	}
	return false
}
```

They live in `src/pkg/images`, and Go links whole packages. `src/pkg/images/pull.go` imports `github.com/docker/cli/cli/command`, `github.com/docker/cli/cli/context/docker`, `github.com/docker/cli/cli/flags`, `github.com/moby/moby/client` and go-containerregistry's `daemon` and `layout`. So asking "is this media type an index?" links the Docker CLI and the Moby client.

That cost lands on zarf's own binary too, anywhere the predicates are needed without the pull path - `src/pkg/zoci`'s `LayersFromImages` is one such caller, and it is doing manifest-walking, not daemon work.

### Links to any relevant code

- https://github.com/zarf-dev/zarf/blob/1abf721e0e06014fbc117ea0be0e3a6172924ced/src/pkg/images/common.go#L77-L93 - `IsManifest` and `IsIndex`
- https://github.com/zarf-dev/zarf/blob/1abf721e0e06014fbc117ea0be0e3a6172924ced/src/pkg/images/pull.go#L19-L29 - the `docker/cli` and `moby/moby/client` imports in the same package
- https://github.com/zarf-dev/zarf/blob/1abf721e0e06014fbc117ea0be0e3a6172924ced/src/pkg/zoci/pull.go#L312 - `LayersFromImages`, a caller that needs the predicates and not the daemon

### Additional context

Suggested shape: move the predicates and their `DockerMediaType*` constants into a leaf package - `src/pkg/images/mediatype`, or alongside the descriptor helpers already in `src/pkg/oci/utils.go` - and re-export from `src/pkg/images` for compatibility. A file move plus re-exports; no behaviour change and no caller churn.

Worth noting this is the same class of thing as the `oci.FetchFile` request filed separately: a small primitive consumers need, currently reachable only through something much heavier.

Happy to open the PR.
