# Build Flags and Environment

This document explains the compiler flags, linker flags, and environment variables used when compiling the `cargoship` binary, and why each one is set. They are defined in `pkg/utils/build/utils.go`, which the Mage host build path (`magefiles/pkg/build/build.go`) uses.

That package exposes two functions, `LDFlags(version, commit string) string` and `GCFLags() string`. The build site additionally sets `CGO_ENABLED=0` and passes `-trimpath` directly in its `go build` invocation rather than through these shared helpers. `.goreleaser.yaml` and `.github/workflows/e2e.yaml` spell the same flags out inline, so a change here has to be made in those two files as well.

## Why this matters

An unoptimized `go build` of this repo produces a binary well over 130MB on Linux/amd64, mostly because `cargoship` pulls in large transitive dependency trees (`k8s.io/client-go`, `zarf`'s signing stack, cloud SDKs, etc.) and, without `CGO_ENABLED=0`, dynamically links against glibc. The flags below were chosen specifically to keep the shipped binary as small and portable as possible without changing behavior.

Measured impact on a Linux/amd64 build of this repo:

| Configuration                                    | Size    | Linking         |
| :----------------------------------------------- | :------ | :-------------- |
| `go build` with no flags                         | ~136MB  | dynamic (glibc) |
| `+ CGO_ENABLED=0`                                | ~98.5MB | static          |
| `+ -trimpath`                                    | ~98.2MB | static          |
| default gcflags instead of `-l` (for comparison) | ~113MB  | static          |
| `-l` (shipped)                                   | 97.95MB | static          |
| `-l -B -C` (former setting, for comparison)      | 96.86MB | static          |

## Environment variables

### `CGO_ENABLED=0`

Forces a pure-Go, statically-linked binary.

*   **Why:** on a native Linux/amd64 host, Go's default is `CGO_ENABLED=1` whenever a C toolchain is present, which produces a dynamically-linked binary against glibc. This repo doesn't need cgo (no imports rely on it), so leaving it enabled only adds size and a runtime dependency on the host's libc/dynamic linker. Disabling it saved ~28% of binary size in testing and makes binaries fully static and portable across Linux distros/container base images.
*   **Where set:** `env["CGO_ENABLED"] = "0"` in `magefiles/pkg/build/build.go`'s `Binary`.

### `GOOS` / `GOARCH`

Standard Go cross-compilation target selection, set per invocation from the `os`/`arch` arguments passed to the build functions. Not size-related; documented here only because they're set alongside `CGO_ENABLED` in the same env map/chain.

## `go build` flags

### `-trimpath`

Strips local filesystem paths (e.g. `/home/user/git/cargoship/...`) from the compiled binary, replacing them with module paths.

*   **Why:** without it, absolute build-machine paths get embedded in the binary (used for panic traces and debug info paths), which leaks local environment details and hurts build reproducibility. It also shaves a small amount of size (a few hundred KB) since fewer/shorter path strings end up in the binary.
*   **Where set:** directly in the `go build` command string in `magefiles/pkg/build/build.go`.

### `-ldflags` (see `LDFlags` in `pkg/utils/build/utils.go`)

*   **`-s`** - omits the symbol table. Symbols aren't needed at runtime and aren't useful without `-w` anyway; this is one of the two biggest size wins available via linker flags.
*   **`-w`** - omits DWARF debug info. Removes the ability to attach a source-level debugger (`dlv`) to the binary, but this is a release build, not a debug build. Combined with `-s`, this is what turns the ~157MB unstripped analysis build in testing into a much smaller shipped binary.
*   **`-X github.com/colonel-byte/cargoship/config.CLIVersion=%s`** - embeds the release version string at link time.
*   **`-X github.com/colonel-byte/cargoship/config.CLICommit=%s`** - embeds the short git commit SHA at link time.

    These two `-X` flags aren't size-related; they exist so `cargoship version` can report accurate build metadata without a separate version file shipped alongside the binary.

### `-gcflags=all="-l"` (see `GCFLags` in `pkg/utils/build/utils.go`)

Applied to `all` packages (including the standard library and vendored dependencies), not just this module's own code.

*   **`-l`** - disables inlining. Counterintuitively, this *reduces* binary size in this repo: inlining duplicates the inlined function's code at every call site, and with a dependency tree this large, the code-size cost of inlining outweighs the runtime speed benefit for a CLI tool that isn't CPU-bound in a hot loop. Measured: `-l` alone produced a binary ~13% smaller than the same build with default `gcflags` (97.95MB against ~113MB).

    `-l` is the only `gcflags` entry this build sets. `-B` and `-C` were set in an earlier revision and are gone - see [Flags that were removed](#flags-that-were-removed) for why.

    Note that `all=` means changing this flag invalidates the build cache for the entire dependency tree, so the first build after touching it is slow. That is a per-flag-change cost, not a per-build one.

## Flags that were removed

These three were set in an earlier revision of this build and are no longer used.

*   **`-gcflags=all=-B` (disable bounds checking)** - removed. Measured at 1.09MB of 97.95MB, about 1.1%. In exchange it stripped bounds checks from every package in the tree, including the code whose job is parsing input this binary does not control: archive extraction, OCI manifests and layers, registry responses. That is exactly where a bounds check is the mechanism that turns a malformed length field into a panic instead of an out-of-bounds read, and 1% of binary size is not worth giving that up. If a hard-to-diagnose crash ever appeared only in release builds, this flag was the first suspect — one more reason it is gone.
*   **`-gcflags=all=-C`** - removed. It never did anything for size. `go tool compile -help` documents `-C` as "disable printing of columns in error messages": a compiler diagnostic formatting flag with no effect on generated code. An earlier revision of this document claimed it disabled `unsafe.Pointer`/checkptr-adjacent checks; that was wrong.
*   **`-a` (force rebuild of all packages)** - removed. It was justified here as a guard against a stale build cache masking flag changes, but Go's build cache is keyed on build flags, so that situation cannot arise: changing `-gcflags` or `-ldflags` already rebuilds everything those flags affect. All `-a` did was make every build redo the standard library.

## What was deliberately not changed

*   **UPX or other binary compressors** - not used. UPX-compressed binaries unpack themselves into memory at startup, adding a small latency hit, and self-modifying/self-extracting binaries are frequently flagged by antivirus and endpoint security tools. Given `cargoship` operates in cluster-management/infrastructure contexts where such scanning is common, this tradeoff wasn't taken.
*   **Trimming `zarf`/`k8s.io/client-go` dependencies** - these are the largest remaining contributors to binary size (particularly `zarf`'s signing stack, which pulls in `sigstore`/`cosign`/`go-tuf`/cloud SDKs), but they're load-bearing for existing features (image signing, cluster operations) and weren't touched here. Removing them would require dropping or refactoring those features, not just changing build flags.
