# Design: Versioned Config Struct Generation for k3s / RKE2

## Goal

Programmatically generate Go structs (and derived `config.yaml` files) that
represent valid `k3s server`, `k3s agent`, `rke2 server`, and `rke2 agent`
CLI flags — per minor version (e.g. 1.34, 1.35, 1.36) — so we can produce
config files we know a given version will accept, without hand-transcribing
flags from docs.

Hard requirement: the whole pipeline must be buildable and runnable in a
**disconnected/air-gapped network**. No step may require live access to the
Go module proxy, GitHub, or any other network resource at build time.

## Why this isn't a simple "unmarshal into the upstream struct" problem

Neither project unmarshals `config.yaml` into its internal `Agent`/`Server`
structs via `yaml` struct tags. Instead:

- Flags are declared as `urfave/cli` `Flag` values (`cli.StringFlag`,
  `cli.BoolSliceFlag`, etc.), each with a `Destination: &SomeStruct.Field`
  pointer.
- `config.yaml` is parsed generically and translated into equivalent CLI
  args, then fed through the same flag-parsing path as real CLI input.
- The **flag name** (kebab-case, e.g. `cluster-cidr`) is the authoritative
  YAML/CLI vocabulary — not the Go struct field name, which has no `yaml`
  tag and doesn't always match the flag name 1:1.

Conclusion: **the flag list, not the struct, is the source of truth** for
what a valid config.yaml key looks like.

## Key constraint: k3s cannot be safely imported as a library dependency

k3s's own `go.mod` contains extensive `replace` directives pinning forked
versions of `k8s.io/api`, `k8s.io/apiserver`, `containerd`, etc. Go's module
system **does not honor a dependency's `replace` directives** — they only
apply when that module is the main module being built
(see golang/go#30354). This means:

- `go get github.com/k3s-io/k3s/pkg/cli/cmds` into our own module, followed
  by `go mod vendor`, is fragile: it may fail to resolve, pull incompatible
  transitive dependencies, or silently vendor different code than what k3s
  actually ships.
- This risk is version-dependent and not something we can fully verify
  up front — a future k3s release could introduce an import that trips
  this even if earlier versions didn't.

**Decision: do not build or import k3s/RKE2 code at all.** Extract flag
metadata via **static source parsing** (`go/parser` / `go/ast`) instead of
compiling or reflecting over the real package. This sidesteps the
replace-directive problem entirely — we only ever read text, never resolve
a dependency graph.

Tradeoff accepted: we lose compiler-verified type information. Acceptable,
because k3s/RKE2 flag types are plain (`string`, `bool`, `int`,
`cli.StringSlice`) — no exotic `cli.Generic` implementations expected.

## Architecture

Two phases that never touch Go's "one version per build" limitation,
because they operate on different things:

- **Extraction** (per upstream version) — operates on committed *source
  text*, not compiled code. No third-party Go dependency ever appears in
  our own `go.mod`.
- **Consumption** — our own generated packages, one per version, distinct
  Go package paths. Ordinary Go, no conflict, since they're not "the same
  package at two versions" — they're just different packages we generated.

```
/thirdparty-src/
  k3s/
    v1_34/agent.go, server.go, version.go   (raw files, pinned to v1.34.x+k3s1)
    v1_35/...
    v1_36/...
  rke2/
    v1_34/...
    v1_35/...
/extractor/        # our tool: go/ast walker, no third-party deps
/gen/
  v1_34/zz_server_config.go, zz_agent_config.go   # generated, zero deps
  v1_35/...
  v1_36/...
/thirdparty-src/pins.json   # manifest: distro -> repo, files, upstream tags
```

## Workflow

### Adding / refreshing a version (the only step requiring network access)

1. Resolve the exact upstream tag (e.g. `v1.36.0+k3s1`, `v1.36.0+rke2r1`).
2. Pull only the needed raw source files at that tag (`git show
   <tag>:pkg/cli/cmds/server.go`, or a sparse/shallow checkout scoped to
   `pkg/cli/cmds`) — not the whole repo, not a buildable module.
3. Commit those files under `thirdparty-src/<project>/<version>/`.
4. Append the tag to that distro's entry in `thirdparty-src/pins.json`.

This is a small, auditable diff each time (a handful of files), not a
dependency tree. `git diff` between two version directories is a
reasonable first-pass changelog of flag changes between releases.

### Offline steps (extraction → codegen → build)

All pure Go, no network:

1. **Extract**: `go/ast` walks each committed `server.go`/`agent.go`,
   locates the `[]cli.Flag{...}` literal(s) (including flags referenced
   indirectly via package-level vars, e.g. `&SELinuxFlag`), and pulls out
   name, type (scalar vs. slice), default, hidden/deprecated status, and
   the `Destination` field name where present. Emits a JSON manifest per
   version.
2. **Generate**: a `text/template` pass turns each JSON manifest into a
   Go struct with `yaml:"<flag-name>,omitempty"` tags, in its own
   version-namespaced package (`gen/v1_36`, etc.).
3. **Build**: our main module builds normally. It has no dependency on
   k3s/RKE2 at all — at most one small, ordinary vendored dependency
   (e.g. a YAML marshal library) if we want marshal helpers, which is
   trivial to `go mod vendor` since it's a real, un-forked dependency.

## Known edge cases to handle explicitly

- **Slice-typed flags** (`cli.StringSlice`, e.g. `tls-san`, `node-label`)
  must generate as YAML lists, not scalars.
- **Hidden/deprecated flags** (e.g. `disable-agent`, `cluster-secret` are
  marked `Hidden: true` in k3s) exist in the flag list but are often
  aliases for newer flags — decide per-flag whether to emit them into
  generated configs or just track them for reference.
- **Cross-field validation** (e.g. `--disable-apiserver` is incompatible
  with `--datastore-endpoint`) is enforced only in k3s's runtime code, not
  expressed in the flag list. A generated config can be structurally valid
  YAML but still semantically invalid. Out of scope for the generator
  itself; worth a follow-on validation layer if needed.
- **RKE2 depends on a specific k3s version**, not a 1:1 Kubernetes version.
  RKE2's generator needs to resolve both the RKE2 tag and the k3s tag it
  vendors for shared flags.
- **Renames without an explicit `Aliases:` entry** won't be caught
  automatically by a diff — flag as "needs manual review" rather than
  silently treating as added+removed.

## Rough effort estimate

| Piece | Effort |
|---|---|
| `go/ast` flag-slice extractor (incl. indirect var refs, `Destination` unpacking) | 3–5 days |
| JSON manifest → struct templating | ~1 day |
| Slice/hidden/deprecated-flag edge case handling | ~1 day |
| Per-version diffing / changelog view | ~half day (mostly free once manifests exist) |
| Sparse source-pull tooling per version | ~1 day |

## Consumption: wired into `src/types/distrocfg`

`RancherCommon.ConfigureEngine` (`src/types/distrocfg/rancher_common.go`) looks up the
generated struct for the node's distro (`d.ID`, i.e. `k3s`/`rke2`) and `dis.Spec.Version`,
truncated to its minor release, via `gen.Lookup` (`src/pkg/engineconfig/gen/lookup.go`). If a
match exists, every key in the resolved `config.yaml` map is checked against the matching
`ServerConfig`/`AgentConfig` struct's `yaml` tags (`gen.Keys`, reflection-based) — server vs.
agent selected by `host.IsController()` — and any key that isn't a real flag for that
distro/version/target is logged as a warning (a likely typo or version mismatch) **and
deleted** from the map before it's written. If no generated version matches (nobody has
pulled/generated that minor line yet), it logs a warning once and leaves the config
completely untouched — falling back to blindly writing every key exactly as before this
check existed. So: known distro/version -> unrecognized keys are dropped; unknown
distro/version -> nothing is validated or removed. See "Open questions" below for what's
still deferred.

`gen.Registry` (`src/pkg/engineconfig/gen/zz_registry.go`) is generated alongside the per-version
structs by `mage generate:engineConfig` — every distro/version pull gets wired in automatically,
with no hand-maintained import list to keep in sync.

## Open questions / follow-ups

- Do we want a validation layer that encodes the known cross-field
  constraints (disable-apiserver vs datastore-endpoint, etc.), or leave
  that to whoever consumes the generated config?
- Minor-version granularity only (1.34/1.35/1.36), or also track patch
  releases when a flag changes mid-minor?
- Should hidden/deprecated flags be emitted into generated structs at all,
  or tracked separately as metadata only?
- The current consumer (`src/types/distrocfg`) drops unrecognized keys (with a warning) for
  known distro/versions, but doesn't fail the build. Worth revisiting once there's confidence
  false positives (e.g. a flag added in extraction but not yet in a resolvable form) are rare
  enough to make a hard failure safe instead of a silent drop.
