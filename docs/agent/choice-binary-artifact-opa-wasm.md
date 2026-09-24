# Why Binary-Artifacts sits at 9/10 and stays there

[OpenSSF Scorecard's](https://securityscorecards.dev/viewer/?uri=github.com/colonel-byte/cargoship) Binary-Artifacts check scores 9/10 for one finding:

```
Warn: binary detected: vendor/github.com/open-policy-agent/opa/internal/compiler/wasm/opa/opa.wasm
```

This document records why that file is there, why it can't be removed, and why the fix is an annotation rather than a change to `vendor/`.

## The file is not optional

`opa.wasm` sits next to `vendor/github.com/open-policy-agent/opa/internal/compiler/wasm/opa/opa.go`, which does:

```go
//go:embed opa.wasm
//go:embed callgraph.csv
```

`go:embed` compiles the file into the Go binary. It isn't test fixture data or an example asset sitting near source by coincidence — the package does not build without it. There is no `go mod vendor` flag, build tag, or vendor-pruning step that drops an embedded file out of a package that's actually imported; vendoring copies whatever the package needs to compile.

## OPA is a real, reachable dependency

`go.mod` lists `github.com/open-policy-agent/opa v1.20.2 // indirect`. `go mod why github.com/open-policy-agent/opa/v1/rego` traces the chain:

```
src/cmd -> zarf/src/pkg/signing -> cosign/cmd/cosign/cli/verify -> cosign/pkg/cosign/rego -> opa/v1/rego
```

`opa/v1/rego` is what pulls in `opa/internal/compiler/wasm/opa`, which embeds the wasm file. This is cargoship's signature-verification path, not incidental transitive weight — dropping OPA would mean dropping cosign policy verification.

## Un-vendoring was already rejected, for the same reason it's rejected here

[choice-osv-vendor-overrides](choice-osv-vendor-overrides.md) covers the analogous situation for the Vulnerabilities check and reaches the same conclusion for the same reason: the repository is vendored so it builds on the air-gapped management node cargoship runs from, with no route to a module proxy, so `vendor/` has to hold everything the build needs, including files that cost Scorecard points on their own. Un-vendoring `opa/v1/rego` to dodge one file isn't a candidate any more here than it was there.

Patching the vendored tree by hand to strip the file was also considered and rejected: `go mod vendor` recreates `vendor/` from the module cache on every dependency bump (including Dependabot's own re-vendoring), so a hand-edit doesn't survive the next update and there's no generator target analogous to `Dev.Vendor`'s osv-scanner overrides to keep re-applying it — the file's presence is a direct, structural consequence of embedding, not a stray artifact a script can regenerate around.

## The fix is an annotation, not a vendor change

Scorecard v5 added maintainer annotations: a `scorecard.yml` (or `.scorecard.yml` / `.github/scorecard.yml`) at the repo root that attaches one of five predefined reasons — `test-data`, `remediated`, `not-applicable`, `not-supported`, `not-detected` — to a check. It is **informational only**. It does not change the numeric score; it surfaces context in Scorecard's output (and suppresses the matching code-scanning alert) when read with `--show-annotations`.

`.github/scorecard.yml` in this repo annotates `binary-artifacts` with `remediated`: "a binary is needed but it is signed or has provenance," which is the documented example that matches this case — the module (wasm file included) is checksum-pinned by `go.sum`, so its provenance is verified on every build.

## What this does not do

It does not raise the check to 10. Binary-Artifacts stays at 9/10, same as [choice-osv-vendor-overrides](choice-osv-vendor-overrides.md)'s Vulnerabilities ceiling stays below 10 for its own dependency-chain reasons. The annotation exists so a human reading the Scorecard result sees why the finding is there and that it was investigated, not to move the number.
