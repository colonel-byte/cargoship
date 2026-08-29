# Build Flags and Environment

This document explains the compiler flags, linker flags, and environment variables used when compiling the `cargoship` binary, and why each one is set. These are defined in two places that must be kept in sync:

*   `src/pkg/utils/build/utils.go` — used by the Mage host build path (`magefiles/utils.go`).
*   `.dagger/utils/utils.go` — used by the Dagger containerized build path (`.dagger/build-local.go`).

Both expose the same two functions, `LDFlags(version, commit string) string` and `GCFLags() string`, and both build sites additionally set `CGO_ENABLED=0` and pass `-trimpath` directly in their `go build` invocation rather than through these shared helpers.

## Why this matters

An unoptimized `go build` of this repo produces a binary well over 130MB on Linux/amd64, mostly because `cargoship` pulls in large transitive dependency trees (`k8s.io/client-go`, `zarf`'s signing stack, cloud SDKs, etc.) and, without `CGO_ENABLED=0`, dynamically links against glibc. The flags below were chosen specifically to keep the shipped binary as small and portable as possible without changing behavior.

Measured impact on a Linux/amd64 build of this repo:

| Configuration | Size | Linking |
| :--- | :--- | :--- |
| `go build` with no flags | ~136MB | dynamic (glibc) |
| `+ CGO_ENABLED=0` | ~98.5MB | static |
| `+ -trimpath` | ~98.2MB | static |
| default gcflags instead of `-l -B -C` (for comparison) | ~113MB | static |

## Environment variables

### `CGO_ENABLED=0`

Forces a pure-Go, statically-linked binary.

*   **Why:** on a native Linux/amd64 host, Go's default is `CGO_ENABLED=1` whenever a C toolchain is present, which produces a dynamically-linked binary against glibc. This repo doesn't need cgo (no imports rely on it), so leaving it enabled only adds size and a runtime dependency on the host's libc/dynamic linker. Disabling it saved ~28% of binary size in testing and makes binaries fully static and portable across Linux distros/container base images.
*   **Where set:** `env["CGO_ENABLED"] = "0"` in `magefiles/utils.go`'s `hostBuildLocal`, and `WithEnvVariable("CGO_ENABLED", "0")` in `.dagger/build-local.go`.

### `GOOS` / `GOARCH`

Standard Go cross-compilation target selection, set per invocation from the `os`/`arch` arguments passed to the build functions. Not size-related; documented here only because they're set alongside `CGO_ENABLED` in the same env map/chain.

## `go build` flags

### `-trimpath`

Strips local filesystem paths (e.g. `/home/user/git/cargoship/...`) from the compiled binary, replacing them with module paths.

*   **Why:** without it, absolute build-machine paths get embedded in the binary (used for panic traces and debug info paths), which leaks local environment details and hurts build reproducibility. It also shaves a small amount of size (a few hundred KB) since fewer/shorter path strings end up in the binary.
*   **Where set:** directly in the `go build` command string in both `magefiles/utils.go` and `.dagger/build-local.go`.

### `-a`

Forces rebuilding of all packages, including the standard library, rather than reusing cached `.a` files.

*   **Why:** ensures the build flags below (especially `-gcflags`) are actually applied everywhere, since Go's build cache is keyed on flags but a stale cache can otherwise mask flag changes during iteration. Not a size optimization on its own — mainly a correctness/reproducibility guard for a release build.

### `-ldflags` (see `LDFlags` in both `utils.go` files)

*   **`-s`** — omits the symbol table. Symbols aren't needed at runtime and aren't useful without `-w` anyway; this is one of the two biggest size wins available via linker flags.
*   **`-w`** — omits DWARF debug info. Removes the ability to attach a source-level debugger (`dlv`) to the binary, but this is a release build, not a debug build. Combined with `-s`, this is what turns the ~157MB unstripped analysis build in testing into a much smaller shipped binary.
*   **`-X github.com/colonel-byte/cargoship/src/config.CLIVersion=%s`** — embeds the release version string at link time.
*   **`-X github.com/colonel-byte/cargoship/src/config.CLICommit=%s`** — embeds the short git commit SHA at link time.

    These two `-X` flags aren't size-related; they exist so `cargoship version` can report accurate build metadata without a separate version file shipped alongside the binary.

### `-gcflags=all="..."` (see `GCFLags` in both `utils.go` files)

Applied to `all` packages (including the standard library and vendored dependencies), not just this module's own code.

*   **`-l`** — disables inlining. Counterintuitively, this *reduces* binary size in this repo: inlining duplicates the inlined function's code at every call site, and with a dependency tree this large, the code-size cost of inlining outweighs the runtime speed benefit for a CLI tool that isn't CPU-bound in a hot loop. Measured: `-l -B -C` together produced a binary ~13% smaller than the same build with default `gcflags`.
*   **`-B`** — disables bounds checking. Trades a small amount of runtime safety (out-of-bounds slice/array access becomes undefined behavior instead of a panic) for reduced code size and slightly faster execution, on the assumption that this codebase's indexing is already correct and covered by tests.
*   **`-C`** — disables the compiler's automatic detection of `unsafe.Pointer` misuse in some cases (checkptr-adjacent checks). Reduces generated code size at the cost of one category of runtime safety net.

    Because `-B` and `-C` remove safety checks, any regression they'd otherwise catch (out-of-bounds access, pointer misuse) will instead surface as memory corruption or silent wrong behavior. If a hard-to-diagnose crash ever shows up only in release builds and not in `go test`/plain `go run`, try reproducing without these two flags first.

## What was deliberately not changed

*   **UPX or other binary compressors** — not used. UPX-compressed binaries unpack themselves into memory at startup, adding a small latency hit, and self-modifying/self-extracting binaries are frequently flagged by antivirus and endpoint security tools. Given `cargoship` operates in cluster-management/infrastructure contexts where such scanning is common, this tradeoff wasn't taken.
*   **Trimming `zarf`/`k8s.io/client-go` dependencies** — these are the largest remaining contributors to binary size (particularly `zarf`'s signing stack, which pulls in `sigstore`/`cosign`/`go-tuf`/cloud SDKs), but they're load-bearing for existing features (image signing, cluster operations) and weren't touched here. Removing them would require dropping or refactoring those features, not just changing build flags.
