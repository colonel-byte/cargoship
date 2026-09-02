# Why google/go-containerregistry and not distribution/distribution for the in-memory test registry

Cargoship's e2e suite needs a real OCI registry to publish/pull against without a network dependency or a live registry container. `src/test/registry.go` provides `test.SetupInMemoryRegistry(t)`, which starts one in-process on a random localhost port and tears it down via `t.Cleanup`.

ORAS (`oras.land/oras-go/v2`) isn't a candidate here at all -- it's the *client* library Cargoship already uses in `src/pkg/coci` to talk to registries. It has no server implementation, so it can't be "the registry" in a test; it's what the CLI uses to talk to whichever registry the test stands up.

The zarf project's own `src/test/testutil/registry.go` (this package is modeled on it) uses [`github.com/distribution/distribution/v3`](https://github.com/distribution/distribution) -- the reference registry server implementation -- with its `inmemory` storage driver. That was the first thing tried here, and it was rejected:

- **Dependency weight.** `distribution/distribution/v3` wasn't a dependency of Cargoship at all before this (only present transitively in `go.sum`, unused). Adding it as a direct import and running `go mod tidy` pulled in a large new indirect tree: Redis and Prometheus clients, several `go.opentelemetry.io/otel/exporters/*` packages, logging bridges, etc. -- dependencies of the registry's optional storage/metrics backends that Cargoship will never use for an in-memory test double.
- **It broke the build.** That new tree included `go.opentelemetry.io/otel/log v0.21.0`, which is not API-compatible with the `go.opentelemetry.io/otel v1.45.0` core Cargoship already pins elsewhere. `go build ./...` failed inside `go.opentelemetry.io/otel/exporters/stdout/stdoutlog` (`undefined: log.Value`, `v.Kind undefined`, etc.) -- a version-skew problem with no clean fix short of forcing otel versions across the whole module.

[`github.com/google/go-containerregistry`](https://github.com/google/go-containerregistry)'s `pkg/registry` package was used instead. It's the same kind of thing Cargoship's own crane-adjacent tooling already depends on transitively (it was in `go.sum` already, unused), so promoting it to a direct dependency added exactly one new package to `vendor/` and nothing else -- `go mod tidy` only moved the existing entry from indirect to direct. `pkg/registry.New()` returns a plain `http.Handler` with no storage-backend options to drag in a dependency tree: wrap it in a stock `net/http.Server`, `Listen` on `:0`, done.

Net effect: same job (an in-memory OCI-compliant registry for tests), zero new transitive dependencies, no build breakage.
