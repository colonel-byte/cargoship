# Third-Party Engine Source (`thirdparty-src/`)

This document explains what lives under `thirdparty-src/` and why it's handled differently from the rest of the repository.

## What it is

`thirdparty-src/<distro>/<version>/` holds raw, unmodified source files pulled from an upstream engine repo (k3s and RKE2) at an exact tag — e.g. `thirdparty-src/k3s/v1_35/{zz_server.go,zz_agent.go}`, pulled from k3s-io/k3s at tag `v1.35.3+k3s1`. `version` is truncated to the minor version (`v1_35`, not the full `v1.35.3-k3s1` patch tag): patch releases are assumed not to change the flag set, so one pull/generate covers every patch in that minor line. RKE2 additionally pulls `zz_root.go` and `zz_k3sopts.go` — see "RKE2's flag composition" below. Both distros also pull the file that declares their packaged components (`zz_stage.go` for k3s, `zz_types.go` for RKE2) — see "Packaged components (`--disable`)" below. The `zz_` prefix marks these as pulled/tool-managed rather than hand-authored, the same convention used for the generated structs under `pkg/engineconfig/gen`.

These files exist so `pkg/engineconfig/extract` can statically parse them with `go/ast` to recover the `urfave/cli` flag declarations k3s/RKE2 ships for a given version. That, in turn, lets `mage generate:engineConfig` (see `magefiles/gen-engine-config.go`) generate a typed Go struct describing the valid `config.yaml` keys for that version — see `docs/agent/design-config-codegen.md` for the full design rationale.

## Why source files instead of a module dependency

k3s and RKE2 are not safe to `go get`/import as library dependencies: their `go.mod` files carry `replace` directives that would leak into (and corrupt) this module's own dependency graph (see golang/go#30354). Statically parsing their source text with `go/parser` sidesteps that entirely — the files are never compiled, never imported, and never linked into any Cargoship binary. They exist purely as text for the extractor to read.

## Why `thirdparty-src/` has its own `go.mod`

Because these files reference packages (`github.com/urfave/cli/v2`, `github.com/k3s-io/k3s/pkg/version`, ...) that this repository does not vendor, letting the main module's tooling discover them causes real breakage: `go build ./...`, `go vet`, and IDE tooling (gopls) all try to resolve those imports and fail.

`thirdparty-src/go.mod` declares a separate, empty module so the Go toolchain treats `thirdparty-src/` as outside the main module boundary. `go build ./...`/`go vet` from the repo root no longer descend into it. IDEs still open a second module context for it, which can surface unresolved-import diagnostics on the files themselves; the workspace's `.vscode/settings.json` sets `gopls.directoryFilters` to `-thirdparty-src` to stop gopls from loading that module at all.

The module boundary is not honoured by every tool. CodeQL's Go autobuilder walks the whole checkout looking for module roots rather than building the main module, finds this one, and tries to resolve the imports above — mis-resolving some of them (`github.com/k3s-io/k3s` to `v1.21.9`, whose `go.mod` declares its path as `github.com/rancher/k3s`), running `go mod tidy -e`, and downloading modules this repository deliberately does not depend on. CodeQL's `paths-ignore` cannot exclude the directory, because that filter does not apply to languages analyzed by building them, and Go is one. `.github/workflows/codeql.yaml` therefore uses `build-mode: manual` and builds `./src/... .` rather than `./...`, which keeps the toolchain inside the main module. Anything else added to CI that walks for `go.mod` files needs the same treatment.

## Layout

```
thirdparty-src/
  <distro>/                  # k3s or rke2
    <version>/                # e.g. v1_35 (minor version only, see above)
      zz_server.go             # verbatim upstream source
      zz_agent.go
      zz_root.go               # rke2 only -- commonFlag, shared by server/agent
      zz_k3sopts.go            # rke2 only -- K3SFlagOption/copyFlag/dropFlag/hideFlag
      zz_stage.go              # k3s only  -- DisableItems, the packaged components
      zz_types.go              # rke2 only -- DisableItems/CNIItems/IngressItems
      SOURCE.txt               # repo, tag, resolved commit, and pulled file list
  pins.json                    # the pinned tags and file lists, see below
  go.mod                       # module boundary marker, see above
```

## RKE2's flag composition

RKE2 doesn't declare its `config.yaml` flags outright the way k3s does. `zz_server.go`/`zz_agent.go` import k3s's own command and wrap it at runtime via `mustCmdFromK3S(cmd, K3SFlagSet{...})` (defined in `zz_k3sopts.go`): a map, keyed by k3s flag name, of `copyFlag`/`dropFlag`/`hideFlag` (or an inline `{Usage: "...", Hide: true}`-style literal) saying whether that k3s flag survives into RKE2 at all, and whether it's hidden. RKE2 then appends a small literal of its own additions (`serverFlag`/`deprecatedFlags`) and a shared `commonFlag` literal declared in `zz_root.go`.

`mage generate:engineConfig` reproduces this: for each RKE2 target it extracts the sibling k3s manifest (`thirdparty-src/k3s/<version>/`), applies the `K3SFlagSet` transform parsed from RKE2's `zz_server.go`/`zz_agent.go`, then appends RKE2's own additions and `commonFlag`. This is why an RKE2 version's pull must always be paired with a k3s pull at the same minor version.

Some of `commonFlag`'s entries have a computed `Name` (a package constant, e.g. `images.KubeAPIServer`, or a concatenation like `podtemplate.KubeAPIServer + "-extra-mount"`) rather than a string literal. Those aren't statically resolvable to a flag name without also pulling and evaluating `pkg/images`/`pkg/podtemplate`, so they're listed in the generated file's "not resolvable" comment block instead of silently guessed at or dropped.

## Packaged components (`--disable`)

The flag extraction above recovers flag *names*. `disable:` is the one key whose *values* also come from a fixed, version-specific vocabulary — the packaged components (addons) the engine deploys unless told not to — and neither distro declares that vocabulary in the files that hold the `[]cli.Flag{...}` literal, so each pulls one more file for it.

k3s declares it as a comma-separated string const in `pkg/cli/cmds/stage.go`, pulled as `zz_stage.go`:

```go
const DisableItems = "coredns, servicelb, traefik, local-storage, metrics-server, runtimes"
```

There is a second, build-tagged copy of that const in `pkg/cli/cmds/nostage.go` (`//go:build no_stage`) listing a strict subset. Cargoship targets the ordinary `!no_stage` build, so only `stage.go` is pulled.

RKE2 declares three string slices in `pkg/cli/types.go`, pulled as `zz_types.go`:

```go
var (
	DisableItems = []string{"rke2-coredns", "rke2-metrics-server", "rke2-snapshot-controller", ...}
	CNIItems     = []string{"calico", "canal", "cilium", "flannel"}
	IngressItems = []string{"traefik", "ingress-nginx"}
)
```

`zz_server.go` refers to those as `rke2cli.DisableItems` / `rke2cli.CNIItems` when building its usage strings, which is why they can't be recovered from `zz_server.go` alone.

**RKE2's list is a computed union, not a copy of `DisableItems`.** `disableExceptSelected` (in `zz_server.go`) pushes every *un-selected* CNI and ingress chart onto `--disable` at startup — `rke2-<item>` and `rke2-<item>-crd` for each entry of `CNIItems` and `IngressItems` — and `validateCNI` adds a literal `rke2-multus`/`rke2-multus-crd` pair the same way. So `disable: [rke2-ingress-nginx]` is legal even though `rke2-ingress-nginx` appears nowhere in `DisableItems`; this repo's own `example/rke2-*/distro.yaml` relies on exactly that. `mage generate:engineConfig` reproduces the union: `DisableItems`, plus the chart-name pair for each CNI and ingress item, plus every string-literal `clx.Set("disable", ...)` call it finds in `zz_server.go` — sorted and deduplicated so the generated file is stable across runs.

`pkg/engineconfig/extract/components.go` does the parsing: `StringListDecl` handles both the const-string and `[]string{...}` shapes, and `DisableSetCalls` scans for the `Set("disable", ...)` calls.

## Pulling a new version

`thirdparty-src/pins.json` is the single source of truth for what gets pulled: one entry per upstream repo, listing the files to copy and every tag to copy them at.

```json
{
  "distros": [
    {
      "name": "k3s",
      "repo": "https://github.com/k3s-io/k3s",
      "files": ["pkg/cli/cmds/server.go", "pkg/cli/cmds/agent.go", "pkg/cli/cmds/stage.go"],
      "tags": ["v1.36.4+k3s1", "v1.35.8+k3s1"]
    },
    {
      "name": "rke2",
      "repo": "https://github.com/rancher/rke2",
      "files": [
        "pkg/cli/cmds/server.go",
        "pkg/cli/cmds/agent.go",
        "pkg/cli/cmds/root.go",
        "pkg/cli/cmds/k3sopts.go",
        "pkg/cli/types.go"
      ],
      "tags": ["v1.36.3+rke2r1", "v1.35.7+rke2r1"]
    }
  ]
}
```

There is no destination path in the manifest: it is derived from `name` plus the tag's major.minor, so `k3s` at `v1.35.8+k3s1` always lands in `thirdparty-src/k3s/v1_35/`. That keeps the truncation rule described above from drifting out of sync with the tag it was truncated from. A tag that does not start with `vMAJOR.MINOR.PATCH` is rejected rather than guessed at.

`mage generate:pullEngineSource` reads the manifest and, for each pinned tag, clones `repo`@`tag` to a throwaway temp directory, copies out `files` verbatim into the derived directory (prefixed `zz_`), and writes `SOURCE.txt` recording the repo, tag, and resolved commit for traceability. It never runs `go get` or touches this repo's module graph.

You rarely need to edit `pins.json` by hand. `mage generate:latestTag <distro> <vMAJOR.MINOR>` resolves the newest non-RC tag upstream publishes for that minor line, pins it — replacing whatever that line held, or adding the line newest-first if it is new — and re-pulls that version's source if the pin moved, so the manifest and the committed files can never disagree. When nothing moved it prints `unchanged` and writes nothing.

```sh
mage generate:latestTag k3s v1.37   # start tracking a new minor line
mage generate:updatePins            # refresh every line already pinned
```

`generate:updatePins` runs that same cycle over every minor line in the manifest, which is the usual way to answer "are we behind upstream?". Both targets touch the network, and both leave struct generation to you: follow with `mage generate:engineConfig` for any version that moved. The version directory name is sanitized into a valid Go package name (dots become underscores) since it becomes the generated package's directory and name directly.

## Consuming it

`mage generate:engineConfig` walks every `thirdparty-src/<distro>/<version>/` directory and writes generated structs to `pkg/engineconfig/gen/<distro>/<version>/`, alongside a `zz_addons.go` holding that version's `Addons` (valid `disable:` values), `CNIs`, and `IngressControllers`. `zz_addons.go` is written for every version even when nothing was extracted — the generated registry references those vars from every package, so one package without them would not compile. An empty `Addons` means no component source was pulled for that version, and consumers treat it as "no data to check against" rather than "nothing is valid". For k3s (and any distro that declares its flags outright) it parses whichever of `zz_server.go`/`zz_agent.go` it finds directly; for RKE2 it composes as described above. See `docs/dev/mage.md` for the broader `Generate` namespace.

It also (re)writes `pkg/engineconfig/gen/zz_registry.go`, wiring every distro/version it just generated into `gen.Registry` so nothing needs hand-maintaining as new versions are pulled. `types/distrocfg` (`RancherCommon.ConfigureEngine`) uses `gen.Lookup` to find the struct matching a node's distro and minor version, and drops (with a warning) any `config.yaml` key that isn't a real flag for it -- falling back to the previous unvalidated, pass-everything-through behavior with a warning if that distro/version was never pulled/generated. On controllers it also checks each `disable:` value against `Entry.Addons` via `gen.UnknownAddons`; an unrecognized component is warned about but **kept**, since the addon vocabulary is composed rather than read straight off a declaration and dropping the entry would silently deploy something the user asked to be without. See "Consumption: wired into `types/distrocfg`" in `docs/agent/design-config-codegen.md` for the full picture.
