# Running the Test Targets Without Mage

Every `mage test:*` target is a thin wrapper around a `go test` plus a little bit of setup: an environment variable, a temp directory, a container cleanup pass. This page names exactly what each target does, so you can run the same check by hand -- on a box without `mage` installed, in a script, or just to see what's actually happening behind the wrapper. For what each *suite* covers and how to extend it, see [e2e-tests](e2e-tests.md), [e2e-phase-tests](e2e-phase-tests.md), and [fuzz-tests](fuzz-tests.md); this page is only about the wrapper.

`mage.md`'s [Running Mage Directly](mage.md#running-mage-directly-without-the-cli) section covers the other way to skip the installed `mage` binary -- `go run ./magefiles/core test:endToEndCluster` runs the exact same Go function `mage test:endToEndCluster` would, just compiled and invoked directly. The commands below go one level further: they are what that function itself runs, with mage (and `magefiles/core`) out of the picture entirely.

## The two shared helpers

Every `EndToEnd*` target funnels through one of two functions in `magefiles/pkg/testrunner/testrunner.go`:

*   **`RunE2E`** -- builds the binary for the host's OS/arch first, with the same flags and environment `mage build:binary` uses (`magefiles/pkg/build/build.go`): `GOOS`, `GOARCH` and `CGO_ENABLED=0` in the environment, and `go build -trimpath -gcflags=all="-l" -ldflags "-s -w -X ...CLIVersion -X ...CLICommit" -o build/cargoship_<goos>_<goarch> ./cmd/cargoship`. There is no `-a`; see [build-flags](build-flags.md) for why it was removed. Then it calls `RunE2ENoBuild`.
*   **`RunE2ENoBuild`** -- creates `build/tmp`, then runs `go test -timeout=<T> -count=1 -v <pkg>` with `CARGOSHIP_E2E_TMPDIR` and `TMPDIR` both pointed at that directory.

The manual commands below spell both steps out for each target, so `-mod=vendor` is included explicitly even though the mage helper relies on the repo's `go env GOFLAGS` to add it.

## `Test.EndToEnd`

```console
$ ids=$(docker ps -aq --filter "label=io.k0sproject.bootloose.owner=bootloose"); [ -n "$ids" ] && docker rm -fv $ids
$ CGO_ENABLED=0 go build -trimpath -gcflags=all="-l" \
    -ldflags "-s -w -X github.com/colonel-byte/cargoship/config.CLIVersion=unset-development-only -X github.com/colonel-byte/cargoship/config.CLICommit=$(git rev-parse --short HEAD)" \
    -o "build/cargoship_$(go env GOOS)_$(go env GOARCH)" ./cmd/cargoship
$ mkdir -p build/tmp
$ CARGOSHIP_E2E_TMPDIR="$PWD/build/tmp" TMPDIR="$PWD/build/tmp" \
    go test -mod=vendor -timeout=1h -count=1 -v github.com/colonel-byte/cargoship/test/e2e/...
```

Runs both e2e groups, including the example packages that pull real engine artifacts and images. Needs Docker.

## `Test.EndToEndNonCluster`

```console
$ CGO_ENABLED=0 go build -trimpath -gcflags=all="-l" \
    -ldflags "-s -w -X github.com/colonel-byte/cargoship/config.CLIVersion=unset-development-only -X github.com/colonel-byte/cargoship/config.CLICommit=$(git rev-parse --short HEAD)" \
    -o "build/cargoship_$(go env GOOS)_$(go env GOARCH)" ./cmd/cargoship
$ mkdir -p build/tmp
$ CARGOSHIP_E2E_TMPDIR="$PWD/build/tmp" TMPDIR="$PWD/build/tmp" \
    go test -mod=vendor -timeout=30m -count=1 -v -short github.com/colonel-byte/cargoship/test/e2e/noncluster/...
```

The `-short` is what the mage target passes; drop it to include `TestCargoshipCreateExample`, which builds a real example package. Mirrors the `e2e-noncluster` CI job. No Docker needed.

## `Test.EndToEndCluster`

```console
$ ids=$(docker ps -aq --filter "label=io.k0sproject.bootloose.owner=bootloose"); [ -n "$ids" ] && docker rm -fv $ids
$ mkdir -p build/tmp
$ CARGOSHIP_E2E_TMPDIR="$PWD/build/tmp" TMPDIR="$PWD/build/tmp" \
    go test -mod=vendor -timeout=1h -count=1 -v github.com/colonel-byte/cargoship/test/e2e/cluster/...
```

No binary build step -- this suite calls the cargoship packages directly. Needs Docker. This is the full engine-bootstrap walk; CI does not run it, see [e2e-phase-tests](e2e-phase-tests.md#in-ci).

## `Test.EndToEndClusterStage`

Same as `EndToEndCluster`, plus one env var that makes the suite stop before the engine starts:

```console
$ ids=$(docker ps -aq --filter "label=io.k0sproject.bootloose.owner=bootloose"); [ -n "$ids" ] && docker rm -fv $ids
$ mkdir -p build/tmp
$ CARGOSHIP_E2E_TMPDIR="$PWD/build/tmp" TMPDIR="$PWD/build/tmp" CARGOSHIP_E2E_STAGE_ONLY=1 \
    go test -mod=vendor -timeout=30m -count=1 -v github.com/colonel-byte/cargoship/test/e2e/cluster/...
```

This is the one CI actually runs, on every pull request.

## `Test.EndToEndClusterDryRun`

`EndToEndClusterStage`'s setup with `-run` narrowing the suite to the dry-run walk:

```console
$ ids=$(docker ps -aq --filter "label=io.k0sproject.bootloose.owner=bootloose"); [ -n "$ids" ] && docker rm -fv $ids
$ mkdir -p build/tmp
$ CARGOSHIP_E2E_TMPDIR="$PWD/build/tmp" TMPDIR="$PWD/build/tmp" CARGOSHIP_E2E_STAGE_ONLY=1 \
    go test -mod=vendor -timeout=30m -count=1 -v -run 'TestClusterPhases/dryrun' github.com/colonel-byte/cargoship/test/e2e/cluster/...
```

Starts no engine, so it is the fastest way to exercise `--dry-run` against real hosts.

## `Test.EndToEndClusterUpgrade`

`EndToEndCluster` with the upgrade walk turned on and a three-hour budget, because it installs a second complete set of engine images on every node:

```console
$ ids=$(docker ps -aq --filter "label=io.k0sproject.bootloose.owner=bootloose"); [ -n "$ids" ] && docker rm -fv $ids
$ mkdir -p build/tmp
$ CARGOSHIP_E2E_TMPDIR="$PWD/build/tmp" TMPDIR="$PWD/build/tmp" CARGOSHIP_E2E_UPGRADE=1 \
    go test -mod=vendor -timeout=3h -count=1 -v github.com/colonel-byte/cargoship/test/e2e/cluster/...
```

No CI job runs this; it is the only way to cover the upgrade phases.

## `Test.EndToEndZarf`

Deletes any leftover k3d clusters, builds the two zarf module wrapper binaries (`ZarfModules` in `magefiles/pkg/build/zarfmodules.go` - no version is stamped in, since a module file reports what zarf did), then runs the zarf module suite on a 90-minute budget:

```console
$ for c in zarf zarf-local-storage; do k3d cluster delete "$c" 2>/dev/null; done
$ CGO_ENABLED=0 go build -trimpath -o build/zarf_init ./cmd/zarf/init
$ CGO_ENABLED=0 go build -trimpath -o build/zarf_package_deploy ./cmd/zarf/deploy
$ mkdir -p build/tmp
$ CARGOSHIP_E2E_TMPDIR="$PWD/build/tmp" TMPDIR="$PWD/build/tmp" \
    go test -mod=vendor -timeout=90m -count=1 -v github.com/colonel-byte/cargoship/test/e2e/zarf/...
```

Needs `k3d`, a `zarf` on PATH, `ansible-playbook`, Docker, and the network on the first run to pull the packages. Every test skips with a reason when one is missing.

## `Test.CleanZarfClusters`

```console
$ for c in zarf zarf-local-storage; do k3d cluster delete "$c" 2>/dev/null; done
```

The cleanup `EndToEndZarf` runs before it starts, exposed on its own. The cluster names are `K3dClusters` in `magefiles/pkg/testrunner/testrunner.go`. A cluster that is not there is not an error.

## `Test.CleanCluster`

```console
$ ids=$(docker ps -aq --filter "label=io.k0sproject.bootloose.owner=bootloose"); [ -n "$ids" ] && docker rm -fv $ids
```

No test runs; this is only the cleanup step the two cluster targets run before they start, exposed on its own for a run that was killed before teardown.

## `Test.Unit`

```console
$ go test -count=1 $(go list ./... | grep -v '^github.com/colonel-byte/cargoship/test/')
```

Every package except the e2e suites under `test/`, which have runners of their own. The target builds the list with `go list` rather than hardcoding it, so a new package is covered the moment it exists. No binary, no Docker, no temp-dir env vars.

## `Test.Fuzz`

```console
$ go test -count=1 ./fuzz/...
```

No binary, no Docker, no temp-dir env vars -- this replays the committed seed corpus in process, which is the whole reason it's a separate target from the suites above.
