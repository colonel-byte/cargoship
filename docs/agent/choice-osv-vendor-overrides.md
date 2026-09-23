# Why vendor/ carries four generated osv-scanner.toml files

`go mod vendor` copies four files into `vendor/` that have nothing to do with Go: a `package-lock.json` under `golang.org/x/telemetry`, and a `requirements` file each under `github.com/theupdateframework/go-tuf`, `github.com/txn2/txeh` and `go.opentelemetry.io/otel`. Next to each of them now sits a generated `osv-scanner.toml` that tells a scanner to skip that ecosystem. This document records why those four files exist, why they are generated rather than committed by hand, and what they deliberately do not cover.

## What forced it

The [OpenSSF Scorecard](https://securityscorecards.dev/viewer/?uri=github.com/colonel-byte/cargoship) Vulnerabilities check runs osv-scanner recursively over the whole checkout and scores it in `checks/evaluation/vulnerabilities.go` as `MaxResultScore - numVulnsFound`, floored at zero. There is no severity weighting and no reachability analysis: every finding is one point, so ten findings and four hundred score the same. The check read 0 against 42 findings, and `vendor/` supplied 40 of them.

| Manifest                                                            | Ecosystem | Findings | What it pins                                                       |
| ------------------------------------------------------------------- | --------- | -------- | ------------------------------------------------------------------ |
| `vendor/golang.org/x/telemetry/package-lock.json`                   | npm       | 34       | The devDependencies of the web UI x/telemetry serves from its repo |
| `vendor/github.com/theupdateframework/go-tuf/requirements-test.txt` | PyPI      | 5        | The Python harness go-tuf runs its own conformance tests under     |
| `vendor/github.com/txn2/txeh/requirements-docs.txt`                 | PyPI      | 1        | The MkDocs toolchain that builds txeh's documentation site         |
| `vendor/go.opentelemetry.io/otel/requirements.txt`                  | PyPI      | 0        | A `codespell` pin for otel's own spelling check                    |

None of it is a cargoship dependency. Nothing in those files is compiled, executed, installed or shipped; they are in the tree because they happen to sit in the directory of a module cargoship imports Go packages from, and `go mod vendor` copies directories. `govulncheck -mode=source ./...` -- which resolves what the binaries actually call -- reports zero.

So the advisories are true statements about postcss and mkdocs-material, and false statements about cargoship. The check was not measuring this repository.

The otel row is zero findings today and has an override anyway. A `requirements.txt` with one package in it is one CVE away from costing a point, and the whole purpose of the arrangement is that a new advisory upstream is not an event here.

## Deleting the files does not work, and neither does un-vendoring

`rm -rf vendor && go mod vendor` puts all four back; they are part of what the vendoring step produces. Removing them by hand means removing them again after every dependency bump, and a run of `go mod vendor` between the removal and the Scorecard run silently undoes it.

Dropping `vendor/` entirely would have removed the problem at the root and was never a candidate. The tree is vendored so the repository builds on a management node with no route to a module proxy; that constraint is the reason `vendor/` exists at all, and it is not negotiable for a scorecard point.

## The config has to sit beside the manifest

osv-scanner resolves configuration relative to the file it is scanning, not to the root of the scan, and it only consults a single root config when the caller passes `ConfigPath`. Scorecard's `clients/osv.go` builds `osvscanner.ScannerActions` without one. A single `osv-scanner.toml` in the repository root therefore changes nothing at all -- measured, not assumed:

| Arrangement                                   | Score | Findings |
| --------------------------------------------- | ----- | -------- |
| No config                                     | 0     | 42       |
| One `osv-scanner.toml` at the repository root | 0     | 42       |
| One beside each of the four manifests         | 8     | 2        |

That is the entire reason the files are scattered through `vendor/` instead of sitting in one obvious place at the top of the repository, which is what anyone would try first.

## Ecosystem-wide overrides, not lists of advisory IDs

`[[IgnoredVulns]]` suppresses named advisories and works -- it took the count from 42 to 41 when pointed at the one txeh finding. It was rejected because the claim being made here is not "these particular advisories are harmless". It is "this ecosystem is not part of the build", which is a property of the manifest and stays true as the advisory list changes. An ID list would need a commit every time a new CVE landed against a lockfile nobody here reads, and each of those commits would look like a security decision while being pure bookkeeping.

`[[PackageOverrides]]` with only `ecosystem` and `ignore = true` says the intended thing once. Each generated file carries a `reason` and a comment explaining itself to whoever finds it while reading vendored third-party source.

## Generated by mage, not committed by hand

`magefiles/vendor-osv.go` holds the four entries and the text they render to; `Dev.Vendor` writes them immediately after `go mod vendor` recreates the tree, so the normal way of updating dependencies leaves them in place without anybody remembering. Committing them as ordinary files was the alternative, and it fails the first time somebody runs `mage dev:vendor` and commits the result.

`Dev.VerifyVendor` is the guard, wired into `check-go-mod.yaml`. It fails if a declared override is missing or has drifted from the generator, and -- the half that matters more -- it fails if `vendor/` contains a non-Go dependency manifest that no entry covers. Without that second check, a dependency bump that happens to pull in a module carrying a `Gemfile.lock` would drop the score on some later Scorecard run with nothing in the diff to point at. It fails on the commit that caused it instead.

`Dev.VerifyVendor` also fails when a manifest an entry names has gone away, which is what turns a stale entry into a build error rather than a file quietly doing nothing.

## Dependabot deletes them, so a workflow puts them back

Generating the overrides from `Dev.Vendor` assumed the normal way of updating a dependency goes through mage. It does not. Dependabot re-runs `go mod vendor` itself for every gomod update, the recreated tree arrives without the four files, and `Dev.VerifyVendor` fails the pull request. #464 and #465 each needed a hand-written `ci: readd the osv scanner artifacts` commit on the Dependabot branch, and #463 exported `Dev.WriteOSVOverrides` so that commit could be made without a pointless re-vendor.

`.github/workflows/fix-vendor-osv.yaml` makes that commit instead. It is the one workflow here that runs on `pull_request_target`, because it needs a token that can push to the pull request branch and Dependabot's `pull_request` runs are handed a read-only one. That trigger is also why the job never builds or runs anything out of the pull request: it checks the base branch and the pull request out side by side, runs `Dev.WriteOSVOverrides` against the base tree only, and copies the four results across. Running the pull request's own code would hand a write token to whatever a freshly bumped dependency happens to ship, which is the specific failure `pull_request_target` is known for. Generating from base is sound because the file contents come from `osvOverrides` in `magefiles/vendor-osv.go`, and a dependency bump never touches that file.

The push uses the release-please GitHub App rather than `GITHUB_TOKEN`, which is not a preference: a push authenticated with `GITHUB_TOKEN` raises no `pull_request` event, so `validate-go-mod` would stay red on the superseded commit and the pull request would still not be mergeable.

Two cases are deliberately left to fail. A bump that drops a module entirely leaves no directory to copy into, and a bump that pulls in a module carrying an uncovered lockfile trips the second half of `Dev.VerifyVendor`. Both need `osvOverrides` edited by a person, so the workflow skips them and lets the check report.

## What this does not do

It does not raise the check to 10. What remains is Go, and Go is where an override would be wrong: an advisory against a module that is actually in the binary is the thing this check exists to report.

| Advisory        | Module                        | Status                                                                                                                                                                                                |
| --------------- | ----------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GO-2026-6225`  | `docker-credential-acr-env`   | Genuinely linked, via `src/cmd` -> zarf signing -> cosign CLI options. Fixed in: N/A                                                                                                                  |
| `GO-2026-5932`  | `golang.org/x/crypto/openpgp` | Not in the build: `go mod why` reports the main module does not need the package. Fixed in: N/A                                                                                                       |
| `CVE-2025-8556` | `cloudflare/circl`            | Already fixed upstream. The advisory carries only a commit range and no version range, and v1.6.5 is 151 commits past the fix, so the match is a defect in the advisory rather than in the dependency |

None of the three has a fixed version to move to, so the ceiling is 7. Expect it to move between 7 and 8 without anything here changing: the circl entry is only matched when the OSV snapshot a given Scorecard run downloaded happens to carry it, and it appeared and disappeared between two scans an hour apart while this was being written. A score that oscillates by one for that reason is not a signal, and chasing it with an `[[IgnoredVulns]]` entry for a Go advisory would be the wrong trade -- the honest fix is to get the version range filed against the advisory.

More broadly, none of this makes anything safer. `govulncheck -mode=source ./...` reported zero before the overrides existed and reports zero after; what changed is that a check which was describing x/telemetry's frontend is now describing cargoship. That is worth the four files and the mage target, and it is worth being clear that it is a measurement fix and not a security fix. The maintenance cost is real and lands on whoever next adds a dependency that carries a lockfile -- `Dev.VerifyVendor` exists so that person gets a clear failure rather than a mystery.
