# Running the Test Targets Without Mage

Every `mage test:*` target is a thin wrapper around a `go test` plus a little bit of setup: an environment variable, a temp directory, a container cleanup pass. This page names exactly what each target does, so you can run the same check by hand -- on a box without `mage` installed, in a script, or just to see what's actually happening behind the wrapper. For what each *suite* covers and how to extend it, see [e2e-tests](e2e-tests.md), [e2e-phase-tests](e2e-phase-tests.md), and [fuzz-tests](fuzz-tests.md); this page is only about the wrapper.

`mage.md`'s [Running Mage Directly](mage.md#running-mage-directly-without-the-cli) section covers the other way to skip the installed `mage` binary -- `go run ./magefiles/core test:endToEndCluster` runs the exact same Go function `mage test:endToEndCluster` would, just compiled and invoked directly. The commands below go one level further: they are what that function itself runs, with mage (and `magefiles/core`) out of the picture entirely.

## The two shared helpers

Every `EndToEnd*` target funnels through one of two functions in `magefiles/pkg/testrunner/testrunner.go`:

*   **`RunE2E`** -- builds the binary for the host's OS/arch first (`go build -a -trimpath -gcflags=... -ldflags=... -o build/cargoship_<goos>_<goarch> ./main.go`, the same flags `mage build:binary` uses), then calls `RunE2ENoBuild`.
*   **`RunE2ENoBuild`** -- creates `build/tmp`, then runs `go test -timeout=<T> -count=1 -v <pkg>` with `CARGOSHIP_E2E_TMPDIR` and `TMPDIR` both pointed at that directory.

The manual commands below spell both steps out for each target, so `-mod=vendor` is included explicitly even though the mage helper relies on the repo's `go env GOFLAGS` to add it.

## `Test.EndToEnd`

```console
$ ids=$(docker ps -aq --filter "label=io.k0sproject.bootloose.owner=bootloose"); [ -n "$ids" ] && docker rm -fv $ids
$ go build -a -trimpath -o "build/cargoship_$(go env GOOS)_$(go env GOARCH)" main.go
$ mkdir -p build/tmp
$ CARGOSHIP_E2E_TMPDIR="$PWD/build/tmp" TMPDIR="$PWD/build/tmp" \
    go test -mod=vendor -timeout=1h -count=1 -v github.com/colonel-byte/cargoship/test/e2e/...
```

Runs both e2e groups, including the example packages that pull real engine artifacts and images. Needs Docker.

## `Test.EndToEndNonCluster`

```console
$ go build -a -trimpath -o "build/cargoship_$(go env GOOS)_$(go env GOARCH)" main.go
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
