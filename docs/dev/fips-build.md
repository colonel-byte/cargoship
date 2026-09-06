# Network Isolation and FIPS 140-3 Builds

This document describes two build-time hardening changes applied to every compiled binary: forcing builds to never reach the network, and enabling Go's native FIPS 140-3 crypto module.

## Network-Disconnected Builds

All build inputs (dependencies, `go.sum` checksums) are already vendored into `/vendor`, so a build should never need to reach the network. To make sure any accidental network access (a stray module fetch, a sumdb lookup, a toolchain auto-download) fails fast instead of silently succeeding or masking a vendoring mistake, every build sets:

*   `GOFLAGS=-mod=vendor` — forces the build to resolve packages from `/vendor` instead of the module cache.
*   `GOPROXY=off` — disables the module proxy outright.
*   `GOSUMDB=off` — disables checksum database lookups.
*   `GOTOOLCHAIN=local` — disables Go's automatic toolchain download/switch, which would otherwise fetch a newer toolchain over the network if `go.mod` requests one.

This is not true container-level network isolation (Dagger's SDK doesn't currently expose a way to disable network access for a container or `WithExec` step); it's a Go-toolchain-level guarantee that the build itself can't depend on the network, which is the part that actually matters for reproducibility.

Applied in `.dagger/build-local.go` on the Dagger builder container, unconditionally for every `BuildLocal` invocation.

## FIPS 140-3 Crypto Module

Go 1.24+ ships a native FIPS 140-3 validated crypto module, controlled by the `GOFIPS140` build-time environment variable (see the [upstream docs](https://go.dev/doc/security/fips140)). All Cargoship builds set:

```
GOFIPS140=latest
```

which compiles in the latest available FIPS 140-3 module and defaults `GODEBUG=fips140=on` at runtime, restricting the binary's crypto operations to FIPS-approved algorithms and modes.

This was chosen over the older, Google-internal `GOEXPERIMENT=boringcrypto` mechanism:

*   `GOEXPERIMENT=boringcrypto` requires `CGO_ENABLED=1` and links against a prebuilt BoringSSL object file that only ships for `linux/amd64` and `linux/arm64` — it can't be used for the `darwin` and `riscv64` targets Cargoship also builds for.
*   It's explicitly documented upstream as unsupported outside Google, with no compatibility guarantees between Go versions.
*   `GOFIPS140` is native (no cgo, no C toolchain needed, no prebuilt platform-specific blob), works identically across every OS/arch Cargoship targets, and is the path Go's own documentation now recommends.

`GOFIPS140=latest` is set unconditionally (not behind a flag or opt-in target) in both development build paths, and in the FIPS half of the release build:

| Build path | File | Notes |
| :--- | :--- | :--- |
| Dev-box host build | `magefiles/utils.go` (`hostBuildLocal`) | Set alongside `GOOS`/`GOARCH` in the build's env map. |
| Dagger container build | `.dagger/build-local.go` (`BuildLocal`) | Set on the builder container, same as the network-isolation env vars above. |
| Release build | `.goreleaser.yaml` (`cargoship-fips` build) | Set alongside the existing `CGO_ENABLED=0`. |

### Release artifacts: FIPS and non-FIPS

Releases ship every OS/arch target twice, from two GoReleaser build definitions that are identical except for `GOFIPS140`:

| Variant | Build id | Archive | Package |
| :--- | :--- | :--- | :--- |
| Stock crypto | `cargoship` | `cargoship_<Os>_<Arch>.tar.gz` | `cargoship` |
| FIPS 140-3 | `cargoship-fips` | `cargoship_fips_<Os>_<Arch>.tar.gz` | `cargoship-fips` |

Both install the binary as `cargoship`, so the two packages declare `provides: cargoship` and conflict with each other — only one can be installed at a time. Use the verification steps below to confirm which variant an artifact is.

### Verifying a binary was built with FIPS 140-3 enabled

**1. Build metadata (`go version -m`)** — the fastest check. Confirms `GOFIPS140` was set at compile time and that `fips140=on` is the binary's default `GODEBUG`:

```sh
$ go version -m build/cargoship_linux_amd64 | grep -i fips
	build	DefaultGODEBUG=fips140=on
	build	GOFIPS140=latest
```

**2. Symbol table (`go tool nm`)** — confirms the FIPS module code is actually linked in, not just requested. Requires a build without `-s -w` (the release/mage builds strip symbols, so run this against an ad-hoc unstripped build if you need it):

```sh
$ go build -o /tmp/cargoship_unstripped ./main.go   # no -ldflags "-s -w"
$ go tool nm /tmp/cargoship_unstripped | grep -c "crypto/internal/fips140"
2306
```

A non-zero count means `crypto/internal/fips140` packages (the actual FIPS module implementation) are present in the binary, not just the build tag.

**3. Runtime enforcement (`GODEBUG=fips140=only`)** — the strongest check. `only` makes the runtime refuse any crypto operation that isn't FIPS-approved, instead of just defaulting to FIPS-approved algorithms (`on`, the binary's default). Running the binary this way and having it work normally shows its actual crypto usage stays within the FIPS-approved set:

```sh
$ GODEBUG=fips140=only ./build/cargoship_linux_amd64 version
0.14.0
```

If the binary used any non-approved algorithm or mode, this would panic instead of running cleanly.
