# Why Dependabot's pull request titles are exempt from `header-max-length`

`PR Title Check` lints a pull request's title with commitlint, because release-please squash-merges and the title becomes the final commit message. `@commitlint/config-conventional` caps a header at 100 characters, and ten of the modules in `go.mod` cannot produce a `build(deps): bump <module> from X to Y` title that fits. Dependabot has no way to shorten a module path, so every bump of one of those ten failed a required status check until somebody edited the title by hand. `.github/commitlint/.commitlintrc.dependabot.yaml` turns that one rule off for Dependabot and leaves it on for everybody else. This document records why the exemption is scoped that narrowly, and why the two changes that look like the obvious fix both make things worse.

## What forced it

[Run 38300320379](https://github.com/colonel-byte/cargoship/actions/runs/38300320379), on the branch that became [#722](https://github.com/colonel-byte/cargoship/pull/722), reports one problem:

```
⧗   --- input ---
build(deps): bump github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding from 1.13.19 to 1.13.20
✖   header must not be longer than 100 characters, current length is 103 [header-max-length]
```

`PR Title Check` failed four times on that pull request across three head commits, because every `synchronize` re-lints a title that has not changed. It passed at 20:53 on the same commit it had failed on at 19:50 - an `edited` event, which is a person renaming the pull request. Four other commits on `main` carry the same `/.../` elision made by hand: [#660](https://github.com/colonel-byte/cargoship/pull/660), [#649](https://github.com/colonel-byte/cargoship/pull/649), [#639](https://github.com/colonel-byte/cargoship/pull/639) and [#638](https://github.com/colonel-byte/cargoship/pull/638).

Ten of the 451 modules in `go.mod` overflow the cap, and they are not a random ten - they are the transitive trees that bump most often, which is why this came up five times in three weeks. Lengths below are for a 7-character version pair such as `1.13.19`, the worst case; the otel bump in #660 measured 115 rather than 117 because its versions are a character shorter.

| Title length | Module                                                                        |
| ------------ | ----------------------------------------------------------------------------- |
| 117          | `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc` |
| 113          | `github.com/AliyunContainerService/ack-ram-tool/pkg/credentials/provider`     |
| 107          | `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc`           |
| 106          | `github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/internal`            |
| 105          | `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc`             |
| 104          | `github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azkeys`              |
| 104          | `github.com/AliyunContainerService/ack-ram-tool/pkg/ecsmetadata`              |
| 103          | `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`               |
| 103          | `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`               |
| 101          | `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`                 |

None of them is a direct dependency. All ten arrive through the vendored cosign, Azure and otel trees, so there is no import to rewrite and no module to drop: the names are what they are.

## Grouping lengthens the title it was supposed to shorten

The gomod entry in [`.github/dependabot.yaml`](../../.github/dependabot.yaml) had seven groups until `934972b78` removed them, and re-adding one looks like it would collapse these titles to `build(deps): bump the opentelemetry group with N updates`. It would, on a day when the group has more than one member. On a day when it has exactly one, Dependabot keeps the full module name and appends the group instead:

```
build(deps): bump k8s.io/streaming from 0.37.0 to 0.37.1 in the k8s group
build(deps): bump github.com/ulikunitz/xz from 0.5.16 to 0.5.17 in the misc group
```

That is roughly twenty characters added to the form that was already too long. A group containing one long module is strictly worse than no group at all, and a single-module day is the common case for the otel and aws trees - which is exactly when the cap bites. Grouping is a reasonable answer to pull request volume; it is not an answer to title length.

## The job cannot be skipped for Dependabot

[`.github/workflows/commitlint.yaml`](../../.github/workflows/commitlint.yaml) carried `# if: github.event.pull_request.user.login != 'dependabot[bot]'` as a commented-out line on the `title_check` job from `920e51591` until this change. Uncommenting it is the smaller diff and the wrong one: `title_check` is a required status check on the `main` ruleset, alongside `validate-go-mod`, `build-e2e`, `e2e-noncluster`, `prek` and `lint`. A job that never runs is a result the ruleset and Dependabot's auto-merge may wait on forever, which trades a red check for a stuck pull request. The `merge_group` trigger on that workflow exists for the same reason - the job runs and reports success on a ref that has no pull request, rather than not running.

Keeping the job and swapping its config keeps `title_check` reporting on every event, and keeps Dependabot's titles linted for everything except the rule it cannot satisfy. `type-enum`, `subject-empty` and the rest still apply, so a malformed title from Dependabot fails as loudly as one from a person.

## The second config repeats a rule instead of extending the first

`.commitlintrc.dependabot.yaml` names `@commitlint/config-conventional` and re-states `subject-case`, rather than extending its sibling:

```yaml
extends:
  - "./.commitlintrc.yaml"
```

commitlint resolves an `extends` entry relative to the config file's own location, which is why both files live in `.github/commitlint/` beside the `node_modules` that `npm ci` creates - see [`choice-binary-artifact-opa-wasm.md`](choice-binary-artifact-opa-wasm.md) for how they got there. But a relative `extends` goes through `@commitlint/resolve-extends`, whose loader is built around JS and JSON modules, and a YAML sibling is neither. Duplicating one rule across two short files is cheaper than depending on behaviour that would have to be proven first, and `.commitlintrc.yaml` holds exactly one rule, so the copy is two lines. Both files carry a comment pointing at the other, because the duplication is the thing that will rot: a rule added to one and not the other means Dependabot and people are linted by different standards without anybody deciding that.

## What this does not fix

`Check for go mod changes` still fails on the first run of every gomod pull request, and that is unrelated and deliberate. `go mod vendor` deletes the four generated `osv-scanner.toml` files under `vendor/`, `dev:verifyVendor` fails because of it, [`fix-vendor-osv.yaml`](../../.github/workflows/fix-vendor-osv.yaml) pushes them back, and the rerun passes. The `cancelled` Unit tests, E2E cluster and CodeQL runs on a Dependabot branch's first commit are that push superseding the in-flight ones. Both are self-healing and need nobody; the header comment on [`check-go-mod.yaml`](../../.github/workflows/check-go-mod.yaml) explains the sequence.

## When this entry can go away

Raise the floor rather than delete the file: this stops mattering only if every one of the ten modules leaves the graph, which means cosign, the Azure keyvault SDK and the otel exporters all leaving it. More likely the file outlives the problem quietly. If `@commitlint/config-conventional` ever gains a rule that needs to differ between the two configs, that is the point to revisit whether a shared JS base is worth the resolution it would buy.
