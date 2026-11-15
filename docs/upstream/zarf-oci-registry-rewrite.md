# Request: drop the inherited `defenseunicorns` registry rewrite from `src/pkg/oci`

- **Upstream**: [`zarf-dev/zarf`](https://github.com/zarf-dev/zarf)
- **Package**: `src/pkg/oci`
- **Read against**: v0.88.0, the release vendored here
- **Status**: landed upstream - PR [#5504](https://github.com/zarf-dev/zarf/pull/5504), commit [`2bb9b35`](https://github.com/zarf-dev/zarf/commit/2bb9b35), merged 2026-10-21. Not in v0.88.0.
- **Currency check**: `main` @ `2904d9e`, 2026-10-23 - answered, see below
- **Verified**: not prototyped - the preferred form is a deletion, so there is nothing to prove beyond that the tree still builds without those nine lines

## This has landed upstream

The `🦄`/`defenseunicorns` branch is gone from `setRepository` on `main`, removed in "feat!: remove defense unicorn shortcuts from oci package refs" - the preferred option on this page rather than the `WithRegistryAliases` fallback. The `docker.io` -> `registry-1.docker.io` rewrite is kept, which is also what this page asked for. It is marked a breaking change, so it arrives announced rather than quietly.

Not in v0.88.0, so the nine lines quoted below are still in the vendored copy here. Nothing needs filing; the rest of the page is kept as the record of the ask. See the method note in [`zarf-oci-credential-store.md`](zarf-oci-credential-store.md) on reading `main` rather than the vendored release.

## What was awkward in v0.88.0

`setRepository` silently rewrites two registry names (`src/pkg/oci/common.go:159-167`):

```go
// patch docker.io to registry-1.docker.io
// this allows end users to use docker.io as an alias for registry-1.docker.io
if ref.Registry == "docker.io" {
	ref.Registry = "registry-1.docker.io"
}
if ref.Registry == "🦄" || ref.Registry == "defenseunicorns" {
	ref.Registry = "ghcr.io"
	ref.Repository = "defenseunicorns/packages/" + ref.Repository
}
```

The `docker.io` line is uncontroversial - it is the documented alias for Docker Hub's registry endpoint, and every OCI client does something equivalent.

The second is vendor branding, and it arrived here by inheritance rather than by decision: this file came in-tree from `github.com/defenseunicorns/pkg/oci`, where the shorthand made sense for that organisation's own tooling. In `zarf-dev/zarf` it means a reference to a registry literally named `defenseunicorns` - or to the emoji `🦄` - resolves somewhere else entirely, with the repository path rewritten underneath it, and nothing in the error path or the logs says so.

The practical problem is not that anybody types `🦄`. It is that a general-purpose OCI client silently redirecting a user-supplied registry name to a third party's namespace is surprising in exactly the situation where surprise is expensive, and it is undiscoverable from the package's exported API.

## Proposed change

Preferred: delete the `🦄`/`defenseunicorns` branch. If anything in `zarf-dev` still depends on the shorthand, it belongs in the CLI layer that parses user input, not in the library every consumer links.

If it has to keep working, make it opt-in and name it, so it is visible at the call site:

```go
// WithRegistryAliases enables shorthand registry names such as "defenseunicorns" and "🦄".
func WithRegistryAliases(enabled bool) Modifier
```

Either way, the rewrite is worth a `Debug` line when it fires. `setRepository` already holds a logger (`o.log`) and already logs the credential lookup a few lines below, so a reference that got rewritten should say so on the same channel.

## Why this is in upstream's interest

It is dead branding in a package that has changed owners. Zarf has already done this kind of cleanup deliberately - [ADR 0019](https://github.com/zarf-dev/zarf/blob/main/adr/0019-decouple-transformation-library-from-zarf.md) decoupled the transformation library from zarf for the same reason - and `src/pkg/oci` is now the surface zarf offers library consumers, so an inherited special case for a different organisation's registry reads as a leftover.

## Evidence that it is needed

Cargoship (`colonel-byte/cargoship`) imports `src/pkg/oci` and publishes to operator-supplied registries inside air-gapped enclaves, where the registry name comes out of a config file or a `--registry` flag. Nothing in our fleet is named `defenseunicorns`, so this costs us nothing today - it is a latent surprise rather than a live bug, which is why it is filed as a cleanup and not a defect.

The `docker.io` rewrite we would keep, and we would not want it gated behind the same option: it is correct, expected, and not branding.
