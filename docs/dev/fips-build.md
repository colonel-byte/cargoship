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

Container images are paired the same way. Each of the two image flavours ([static and UBI](goreleaser.md#container-images)) ships in both crypto variants, so a release publishes four manifests on `ghcr.io/colonel-byte/cargoship`:

| Variant | Static image | UBI image |
| :--- | :--- | :--- |
| Stock crypto | `:<tag>`, `:latest` | `:<tag>-ubi` |
| FIPS 140-3 | `:<tag>-fips`, `:latest-fips` | `:<tag>-ubi-fips` |

The two halves of a pair share one Dockerfile and differ only in which build artifact GoReleaser feeds them — the FIPS binary installs under the same name and path, so entrypoint, user, `$HOME`, and `/workspace` are identical across all four. Picking the FIPS variant is a pure tag swap.

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

### Verifying a container image was built FIPS-enabled

The image adds no FIPS machinery of its own — it carries the binary verified above, so these checks are the same three checks reached through the image. Examples use the snapshot tags a local `--snapshot` run produces (`:v0.19.0-fips-amd64`); against a real release, drop the `-amd64` platform suffix and use the published tag.

**1. The `fips140` label** — the cheapest triage, and the only check that works without pulling layers or running anything:

```sh
$ docker image inspect ghcr.io/colonel-byte/cargoship:<tag>-fips \
    --format '{{ index .Config.Labels "io.github.colonel-byte.cargoship.fips140" }}'
true
```

Set from `.goreleaser.yaml` on all four images, `"true"` on the FIPS pair and `"false"` on the stock pair. It is stated on both sides on purpose: a missing label is ambiguous between a stock image and one built before the variants existed, so treat absence as "unknown", not "no".

Being a label, it records what the release config *claimed*, not what the binary *is*. It is a routing signal for scanners and admission policy; use checks 2–4 when you need the actual property.

**2. Runtime enforcement (`GODEBUG=fips140=only`)** — the strongest check, and the one to reach for first, since it needs nothing but `docker run`. As with the bare binary, `only` makes the runtime refuse any crypto operation outside the FIPS-approved set, so a clean run is real evidence:

```sh
$ docker run --rm -e GODEBUG=fips140=only ghcr.io/colonel-byte/cargoship:<tag>-fips version
0.19.1-snapshot
```

A stock image under the same variable does not necessarily fail on `version` alone — it fails once it reaches a non-approved primitive. Exercise a code path that actually does crypto for a conclusive negative.

**3. Build metadata of the shipped binary (`go version -m`)** — proves `GOFIPS140` was set at compile time for the binary that is genuinely inside the image. Neither image has a shell, `go`, or (for the static one) any way to introspect itself, so copy the binary out and inspect it on the host. Note the two variants keep the binary at different paths, per the packaging difference described in the [goreleaser doc](goreleaser.md#ubi-variant):

```sh
$ cid=$(docker create ghcr.io/colonel-byte/cargoship:<tag>-fips)   # static: /usr/local/bin
$ docker cp "$cid:/usr/local/bin/cargoship" /tmp/from-image && docker rm "$cid"
$ go version -m /tmp/from-image | grep -i fips
	build	DefaultGODEBUG=fips140=on
	build	GOFIPS140=latest
```

```sh
$ cid=$(docker create ghcr.io/colonel-byte/cargoship:<tag>-ubi-fips)   # UBI: /usr/bin
$ docker cp "$cid:/usr/bin/cargoship" /tmp/from-ubi-image && docker rm "$cid"
$ go version -m /tmp/from-ubi-image | grep -i fips
	build	DefaultGODEBUG=fips140=on
	build	GOFIPS140=latest
```

Run against a stock image, `grep` matches nothing — that empty result is the expected negative, and is what distinguishes the pair.

**4. The installed package (UBI only)** — the UBI image installs the rpm rather than copying the binary, so the rpm database names the variant directly:

```sh
$ docker run --rm --entrypoint rpm ghcr.io/colonel-byte/cargoship:<tag>-ubi-fips -q cargoship-fips
cargoship-fips-0.19.1~snapshot-1.x86_64
```

The stock image answers to `-q cargoship` instead. Since the two packages conflict, exactly one of the two queries succeeds on any given image, which makes this an unambiguous variant check on its own.

**Cross-checking image against archive.** The [binary identity](goreleaser.md#binary-identity) property holds per variant: GoReleaser compiles each variant once and reuses that artifact for its archive, its packages, and both of its images. So the FIPS binary is byte-identical in all three places, and a signature verified against the FIPS archive transitively covers the FIPS images:

```sh
$ sha256sum dist/cargoship-fips_linux_amd64_v1/cargoship /tmp/from-image /tmp/from-ubi-image
f1c536bbfc8657a46e6579f64308a06dee26436c345d807e7f3d6baa7c57a04f  dist/cargoship-fips_linux_amd64_v1/cargoship
f1c536bbfc8657a46e6579f64308a06dee26436c345d807e7f3d6baa7c57a04f  /tmp/from-image
f1c536bbfc8657a46e6579f64308a06dee26436c345d807e7f3d6baa7c57a04f  /tmp/from-ubi-image
```

The FIPS and stock hashes must differ from each other; if they match, the two `dockers_v2` entries are pointing at the same build id and the variants are not really distinct.
