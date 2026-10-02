# Why crypto11 is ignored by Dependabot rather than pointed at its new home

`github.com/ThalesIgnite/crypto11` is pinned at v1.2.5 and will stay there. The upstream repository has been renamed twice and every tag after v1.2.5 declares a different module path, so Dependabot cannot update it and fails its entire daily run trying. `.github/dependabot.yaml` ignores the dependency outright. This document records why the obvious fix - a `replace` directive pointing at the renamed module - was built, tested, and rejected anyway, and why the thing this dependency looks like it affects is not actually in the shipped binary.

## What forced it

The [Dependabot run of 2026-10-02](https://github.com/colonel-byte/cargoship/actions/runs/36991546018) created six update pull requests and then exited 1 on a single dependency:

```
| github.com/ThalesIgnite/crypto11 | go_module_path_mismatch | {
|                                  |                         |   "declared-path": "downloading",
|                                  |                         |   "discovered-path": "github.com/eclipse-keypont/crypto11",
|                                  |                         |   "go-mod": "go.mod"
```

`github.com/ThalesIgnite/crypto11` 301-redirects to `github.com/eclipse-keypont/crypto11`, so the module proxy still answers for the old path and still serves the full tag list. What it cannot do is serve a tag whose `go.mod` agrees with the path it was requested under:

| Tag             | `module` line in its `go.mod`            |
| --------------- | ---------------------------------------- |
| v1.2.5          | `github.com/ThalesIgnite/crypto11`       |
| v1.3.0          | `github.com/ThalesGroup/crypto11`        |
| v1.6.8 (latest) | `github.com/eclipse-keypont/crypto11`    |

v1.2.5 is the last tag published under the original path. Requiring anything newer under that name produces the error Dependabot reported, which reproduces in four lines in a throwaway module:

```
module declares its path as: github.com/eclipse-keypont/crypto11
        but was required as: github.com/ThalesIgnite/crypto11
```

Nothing in this repository requires crypto11 directly. `go mod why -m` traces it to the signing path:

```
cmd -> zarf/src/pkg/signing -> cosign/cmd/cosign/cli/signcommon -> cosign/pkg/cosign/pkcs11key -> ThalesIgnite/crypto11
```

`sigstore/cosign/v3` v3.1.3 is the newest release and its `go.mod` still reads `github.com/ThalesIgnite/crypto11 v1.2.5`, so there is no cosign version to move to that resolves this. The fix has to come from upstream re-importing the renamed module.

## The dependency is not in the shipped binary

This is the fact that decides how much any of it matters, and it is not visible from `go.mod`, where crypto11 looks like an ordinary indirect dependency.

crypto11's only consumer, `cosign/pkg/cosign/pkcs11key`, is split across two files by build tag. `pkcs11key.go` carries `//go:build pkcs11key` and imports crypto11; `disabled.go` carries `//go:build !pkcs11key` and defines the same API as stubs that return `errors.New("unimplemented")`. Nothing in `.github/`, `magefiles/`, or the goreleaser config sets that tag, so every build of cargoship - CI, release, and the e2e suite alike - compiles the stubs:

| Build                  | crypto11 packages in the graph |
| ---------------------- | ------------------------------ |
| `go list -deps ./...`  | 0                              |
| with `-tags pkcs11key` | 1                              |

The release build cannot opt in even by accident. `.goreleaser.yaml` sets `CGO_ENABLED=0`, and crypto11 reaches the PKCS#11 library through `github.com/miekg/pkcs11`, which is `import "C"`. Setting the tag under the release environment does not produce a binary with hardware signing in it, it produces a compile error:

```
$ CGO_ENABLED=0 go build -tags pkcs11key ./...
# github.com/ThalesIgnite/crypto11
vendor/github.com/ThalesIgnite/crypto11/crypto11.go:174:14: undefined: pkcs11.Ctx
```

So crypto11 is a `go.mod`, `go.sum`, and `vendor/` artifact and nothing more: no tag enables it, and the release settings could not build it if one did. PKCS#11 hardware-token signing is the feature it backs, and that feature is compiled out. A stale pin here cannot reach a user, and `govulncheck -mode=source -tags pkcs11key ./...` reports no vulnerabilities either way, so this is Dependabot hygiene and not a security matter.

The corollary for anyone verifying a change to this dependency: `go build -tags pkcs11key ./...` only compiles it on a host where cgo is available and enabled, which is the default on Linux with a C toolchain but is not what CI or the release build does.

Note that the e2e suite does sign and verify packages for real - `TestCargoshipSign`, `TestCargoshipSignedRoundTrip`, and `TestCargoshipInstallVerify` cover 19 subtests against ephemeral cosign key pairs from `cosign.GenerateKeyPair`. That coverage is all file-based keys (`--signing-key <path>`), which never touch crypto11. Good signing coverage is not coverage of this dependency.

## The `replace` directive works, and still does not help

The natural fix is to send the old path at the maintained fork:

```sh
go mod edit -replace github.com/ThalesIgnite/crypto11=github.com/eclipse-keypont/crypto11@v1.6.8
```

This was done in full, not sketched. It resolves, and v1.6.8's API satisfies cosign's v1.2.5-era call sites with no shimming: `go mod tidy`, `go mod vendor`, `go build ./...`, `go build -tags pkcs11key ./...`, `go vet`, `go test -short`, `Dev.VerifyVendor`, `pre-commit run --all-files`, and `Test.EndToEndNonCluster` all passed on it. The approach is sound and it does silence the failure.

It was rejected because it buys nothing over the ignore entry. Dependabot does not update replaced dependencies, by explicit design. `go_modules/lib/dependabot/go_modules/file_parser.rb` filters the dependency set before anything else runs, and `skip_dependency?` opens with:

```ruby
# Updating replaced dependencies is not supported
return true if dependency_is_replaced(dep)
```

The match test treats a directive whose left side carries no version as unconditional, which is this one, so crypto11 is dropped at parse time. Upstream's comment gives the reason: it "prevents that we change dependency versions without any impact since the actual version that is being imported is defined by the replace directive." The replacement target does not get picked up instead - the parser only walks `require` entries, and a replace target is not one. Both names end up invisible.

That leaves the two options doing the same thing by different routes:

| Approach        | Dependabot tracks crypto11? | Run stops failing? | Vendor churn |
| --------------- | --------------------------- | ------------------ | ------------ |
| `ignore` entry  | No, explicitly              | Yes                | None         |
| `replace`       | No, silently                | Yes                | ~800 lines   |

Neither restores automated updates, so the choice is only about which pin goes stale and how loudly. The ignore entry states its own reasoning in a config file someone editing Dependabot behaviour will read. The `replace` skip is invisible unless you already know the parser behaviour above, and it adds a hand-maintained version pin to `go.mod` plus a vendored tree jumping four minor versions, for a package that is compiled out. Given that, the smaller and more legible change won.

The `replace` remains the right move the moment any of this stops being true - if cosign starts calling v1.6.x API, or if something ever sets `-tags pkcs11key`.

## Re-vendoring deletes the osv-scanner overrides

Recorded here because it bites anyone who tries the `replace` route. `go mod vendor` removed all four generated `osv-scanner.toml` files that [choice-osv-vendor-overrides](choice-osv-vendor-overrides.md) describes, and `Dev.VerifyVendor` fails until `go run ./magefiles/core dev:writeOSVOverrides` puts them back. That document covers the arrangement and the workflow that repairs Dependabot's own branches; the only new thing is that a hand-run `go mod vendor` needs the same follow-up.

## The rest of the log is noise, and chasing it is wasted work

The failing run logged 40 `Error while fetching release date info` lines at INFO level. Exactly one of the 24,132 log lines was fatal - the `go_module_path_mismatch` above - and the rest are not this repository's problem:

| Log entry                                           | Count | Cause                                                                                               |
| --------------------------------------------------- | ----- | --------------------------------------------------------------------------------------------------- |
| `invalid version: unknown revision 0.0.0-...`       | 39    | Dependabot's cooldown filter drops the `v` prefix and queries `@0.0.0-...`, which can never resolve |
| `cuelabs.dev/go/oci/ociregistry: ... 403 Forbidden` | 1     | That host refuses Dependabot's egress proxy on `?go-get=1`                                          |

The 39 hit every pseudo-versioned module in the tree - `genproto`, `k8s.io/utils`, `x/telemetry`, `digitorus/pkcs7`, `masterzen/simplexml`, and the rest. The only consequence is that cooldown cannot be applied to those modules; Dependabot logs `Filtered out 0 version(s)` and carries on. Neither row is actionable here, and neither has anything to do with the failure.

## When this entry can go away

Delete the ignore entry when `sigstore/cosign/v3`'s `go.mod` requires `github.com/eclipse-keypont/crypto11` (or whatever path the project is on by then) instead of `github.com/ThalesIgnite/crypto11`. At that point the old path leaves the module graph on its own and the entry becomes a no-op. Until then, a Dependabot run that does not mention crypto11 is the entry working, not the problem having resolved itself.
