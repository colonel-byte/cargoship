# Running the End-to-End Tests

The `noncluster` e2e suite drives the **built `cargoship` binary** as a subprocess, the same way a user would. It does not import the command packages, so what it proves is that the shipped binary behaves, not that the internals compile. Everything below is about that suite and is a plain `go test` invocation, which is what you want when a test fails and you need to poke at it. The `cluster` suite is the other way round -- it calls the cargoship packages directly and needs no binary at all; see [e2e-phase-tests](e2e-phase-tests.md).

## Layout

```
src/test/e2e/noncluster/   misc + package command groups: version, sha256sum, vault-encrypt, create, publish, pull, sign
src/test/e2e/cluster/      the install group: a nine-node bootloose cluster (plus one upload-only node), walked one phase at a time -- install, join, optionally upgrade, then reset
src/test/common.go         the CargoE2ETest harness (e2e.Cargoship) shared by the suites
src/test/bootstrap.go      TestMain's chdir-to-repo-root, with (Bootstrap) and without (BootstrapInProcess) the binary lookup
src/test/registry.go       in-process OCI registry used by the publish/pull/sign tests
```

The suites are separate Go packages so that a group can be selected by package path rather than by test-name filters. The `noncluster` package starts no containers and makes no network calls: it needs nothing but the binary. The `cluster` package needs Docker and takes tens of minutes, and needs no binary; everything below is about `noncluster`, and [e2e-phase-tests](e2e-phase-tests.md) covers the cluster suite and how to extend it when a phase is added.

## Prerequisites

Build the binary first. The suite looks for it at `build/cargoship_<goos>_<goarch>` and `TestMain` aborts immediately if it is missing:

```console
$ go build -mod=vendor -o "build/cargoship_$(go env GOOS)_$(go env GOARCH)" main.go
```

This is enough for the tests. A build without the release linker flags leaves `cargoship version` reporting the unset placeholder values, which the tests tolerate — they assert the fields are present and non-empty, not what they contain.

The binary is **not** rebuilt by `go test`. After changing anything under `src/`, rebuild it, or you will be testing the previous binary against the new expectations.

## Running

```console
$ go test -mod=vendor -count=1 ./src/test/e2e/noncluster/...            # the whole group, about 6 seconds
$ go test -mod=vendor -count=1 -short ./src/test/e2e/noncluster/...     # same, minus the real example package
$ go test -mod=vendor -count=1 -v -timeout=30m ./src/test/e2e/noncluster/...
```

*   `-count=1` disables the test result cache. Without it, a green run is cached and a rebuilt binary will not re-trigger it, since the binary is not one of the inputs `go test` hashes.
*   `-short` skips `TestCargoshipCreateExample`, which builds `example/rke2-cilium` for real and downloads roughly 1.5GB of engine artifacts and images. Everything else uses `testdata/minimal`, an image-free distro that builds into a 386-byte package in milliseconds.
*   `-timeout` defaults to 10 minutes, which is ample for a `-short` run and not necessarily enough for a full one on a cold cache.

### One test, or one subtest

Subtests are named as sentences, and `go test` replaces spaces with underscores in the `-run` pattern:

```console
$ go test -mod=vendor -count=1 -v -run TestCargoshipSign ./src/test/e2e/noncluster/...
$ go test -mod=vendor -count=1 -v -run 'TestCargoshipSign/re-signing_requires_overwrite' ./src/test/e2e/noncluster/...
```

`-run` takes a regular expression matched against each path element, so `-run 'TestCargoship(Publish|Pull)'` selects a couple of suites and `-run 'TestCargoshipSign/.*overwrite.*'` selects by substring when the exact name is a mouthful.

Those patterns are unanchored, which bites here because several suite names are prefixes of others: `-run TestCargoshipSign` also runs `TestCargoshipSignedRoundTrip`, and `-run TestCargoshipPull` also runs `TestCargoshipPullHTTP`. Anchor both elements to isolate exactly one subtest:

```console
$ go test -mod=vendor -count=1 -v -run '^TestCargoshipSign$/^re-signing_requires_overwrite$' ./src/test/e2e/noncluster/...
```

## Environment variables

*   **`CARGOSHIP_E2E_TMPDIR`** — parent directory for the temp dirs the harness creates, one per `e2e.Cargoship` call, plus the shared minimal package. Unset means the system temp directory. Point it at `build/tmp` to keep all test scratch inside the repo, which makes it easy to see what a run left behind: `CARGOSHIP_E2E_TMPDIR=$PWD/build/tmp TMPDIR=$PWD/build/tmp go test ...`.
*   **`TMPDIR`** — respected by the binary itself for anything it does not put under its own `--tmpdir`. Worth setting alongside the above for the same reason.
*   **`CARGOSHIP_E2E_KEEP_REGISTRY_LOG`** — set to any non-empty value to keep the in-memory registry's request log for passing tests as well as failing ones. See "Artifacts and logs" below.
*   **`CARGOSHIP_CONFIG`** — the config file the binary loads. Only `TestCargoshipCreateExample` sets it (to `src/test/e2e/cargoship-config.yaml`); every other test passes flags explicitly so that what is being tested is visible in the test.

## Seeing what the binary actually did

`e2e.Cargoship` runs the binary with `exec.PrintCfg()`, so its stdout and stderr are written through to the test process's stdout and stderr, not into `t.Log`. `go test` buffers that per package and prints it only when the package fails; add `-v` to see it as it happens.

Two flags are appended to every invocation automatically: `--no-color`, so assertions are matching plain text, and `--tmpdir <fresh dir>`, removed when the call returns.

## Artifacts and logs

*   **In-memory registry log.** The registry used by the publish, pull and sign tests writes one line per HTTP request to a file under the user cache directory, `~/.cache/cargoship/e2e-logs/registry-<timestamp>.log` (`$XDG_CACHE_HOME/cargoship/e2e-logs` if that is set), named the way the CLI names its own log files in `logs/`, rather than to stderr where it would bury the test output. A passing test deletes its log; a failing one keeps it and prints the path in the failure output. Set `CARGOSHIP_E2E_KEEP_REGISTRY_LOG=1` to keep the logs of passing tests too — the path is then printed by `go test -v` for every test that started a registry. That log is usually what explains a publish or pull that failed for a non-obvious reason.
*   **Example packages.** `TestCargoshipCreateExample` writes where `src/test/e2e/cargoship-config.yaml` points it, `src/test/e2e/`. Those `.tar.zst` files are gitignored, and they are large — delete them when done.
*   **Per-call temp dirs** are removed when each `e2e.Cargoship` call returns, including on failure, so nothing the binary wrote under `--tmpdir` survives for inspection. To keep it, reproduce the command by hand as described below.

## Debugging a failure

The most useful property of this suite is that every test is a CLI invocation. When one fails, take the command from the test output and run it yourself against the same binary:

```console
$ ./build/cargoship_linux_amd64 sign path/to/package.tar.zst --signing-key cosign.key --verify=always -k cosign.pub --log-level debug
```

Now nothing is cleaned up, `--log-level debug` is available, and you can iterate without recompiling anything.

A few things that have cost time before:

*   **`--verify` needs its value attached.** It is declared with a cobra `NoOptDefVal`, so `--verify always` parses `always` as a positional argument and fails with `accepts 1 arg(s), received 2`. Use `--verify=always`.
*   **A stale binary** produces failures that make no sense against the source you are reading. Rebuild, then rerun with `-count=1`.
*   **Signing keys are generated per test**, so two calls to the keypair helper deliberately produce unrelated keys. A "verification failed" in a test that expects success usually means the wrong pair was threaded through.

To step through the harness itself (not the binary, which is a separate process), delve works on the test package as usual:

```console
$ dlv test ./src/test/e2e/noncluster -- -test.run 'TestCargoshipPull' -test.v
```
