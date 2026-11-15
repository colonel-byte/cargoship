# Issue drafts for `zarf-dev/zarf`

The `zarf-oci-*.md` and `zarf-zoci-*.md` pages in this directory are the analysis: what is awkward in `src/pkg/oci` and `src/pkg/zoci`, what was prototyped, and what it measured. The numbered pages here are the issues derived from them, written in upstream's own templates and ready to pass to `gh issue create --body-file`.

There are two sets of numbered drafts, with different provenance. `01` to `04` come from the analysis pages beside them and concern `src/pkg/oci` and `src/pkg/zoci`. `05` to `08` concern `src/pkg/packager/assemble` and `src/pkg/archive` - how a component file is fetched, extracted and staged - and have no analysis page, because each was found by reading zarf's own code and reproduces against zarf's own published artifacts. They were written zarf-only from the start rather than translated into it.

The split exists because the two audiences differ. The analysis pages lead with what is awkward for cargoship and close with the case for upstream; the issue drafts are zarf-only and name cargoship nowhere. The bar a maintainer stated in the [OpenSSF thread](https://openssf.slack.com/archives/C07AKUMBDMJ/p1792787963719939) is whether a change provides utility to people interacting with zarf packages and workflows, so an issue that argues from a consumer's needs is arguing the wrong case - the gap has to be visible in zarf's own code, or it is not a gap worth filing. Each draft rests on upstream evidence: `zoci.FetchZarfYAML` bypassing `oci.FetchJSONFile`, `src/pkg/zoci` failing to compile from outside the module, the media-type predicates sitting behind `docker/cli`. Prototype measurements are kept because they describe the patch, not the consumer.


## Timing: v1.0.0, 2026-11-09

Upstream is targeting v1.0.0 for 9 November, and a deprecation policy is being drafted alongside it. That makes *breaking versus additive* the property worth checking before filing anything, because it decides whether a change can be taken whenever someone gets to it or has to make a release:

| Draft                                         | Breaking?                                   | Consequence                                                                                                                                                                                     |
| --------------------------------------------- | ------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `01-oci-fetchfile.md`                         | No - pure addition                          | Safe to land either side of v1.0.0; no clock on it                                                                                                                                              |
| `02-zoci-unimportable.md`                     | Depends which shape maintainers pick        | The `src/api` entry point is additive; moving `FetchZarfYAML` removes a symbol. The draft now says so                                                                                           |
| `03-mediatype-predicates.md`                  | No - file move with re-exports kept         | Safe either side; the re-exports are what make that true                                                                                                                                        |
| `04-index-multi-arch.md`                      | Partly - one behaviour change               | `UpdateIndexWithDescriptors` and `FetchIndex` are additive, but making `fetchIndex` tolerate a non-index tag turns an error into a success, which is a behaviour change regardless of signature |
| `05-create-redownloads-archive-per-file.md`   | No - internal to `assemble`                 | Safe either side; no clock on it                                                                                                                                                                |
| `06-extractpath-collides-with-destination.md` | No - internal to `assemble`                 | Safe either side; turns a failure into a success, which is the direction needing no deprecation                                                                                                 |
| `07-archive-format-by-extension-only.md`      | No - a fallback where there is now an error | Safe either side; an input that resolves today resolves the same way                                                                                                                            |
| `08-oci-file-source.md`                       | **Adds a field to `ZarfFile`**              | The second with a real deadline. A field added to `v1alpha1` and `v1beta1` is far cheaper before the API freezes than after                                                                     |

So `04` and `08` are the two with an actual deadline, which in both cases is the opposite of how they read: each is the weakest fit of its set *and* the only one of it that gets harder after v1.0.0. Filing the issue costs nothing and starts the conversation; writing the PR is what can wait.

## The `src/pkg/oci` drafts

| Draft                                                      | Title                                                                            | Status                                                          | Label             | From                                                                                |
| ---------------------------------------------------------- | -------------------------------------------------------------------------------- | --------------------------------------------------------------- | ----------------- | ----------------------------------------------------------------------------------- |
| [`01-oci-fetchfile.md`](01-oci-fetchfile.md)               | Add `oci.FetchFile` for fetching a layer's bytes by path                         | **filed [#5553](https://github.com/zarf-dev/zarf/issues/5553)** | `enhancement ✨`   | [`zarf-oci-fetch-file-by-path.md`](zarf-oci-fetch-file-by-path.md)                  |
| [`03-mediatype-predicates.md`](03-mediatype-predicates.md) | Move `IsManifest` and `IsIndex` into a package that does not link the Docker CLI | **filed [#5554](https://github.com/zarf-dev/zarf/issues/5554)** | none applied      | [`zarf-zoci-reusable-layer-helpers.md`](zarf-zoci-reusable-layer-helpers.md) item 1 |
| [`02-zoci-unimportable.md`](02-zoci-unimportable.md)       | Make `src/pkg/zoci` importable from outside the module                           | held - see below                                                | `project-hygiene` | [`zarf-zoci-reusable-layer-helpers.md`](zarf-zoci-reusable-layer-helpers.md) item 2 |
| [`04-index-multi-arch.md`](04-index-multi-arch.md)         | Support multiple architectures in `oci.UpdateIndexWithDescriptor`                | held - see below                                                | `enhancement ✨`   | [`zarf-oci-index-multi-arch.md`](zarf-oci-index-multi-arch.md)                      |

**On labels.** The `tech_debt.md` template declares `labels: 'tech-debt 💳'`, but that label does not exist in the repository - `enhancement ✨` is the only one of the four template labels that does. Passing it to `gh issue create` fails, so `02` and `03` use `project-hygiene`, the closest label that exists. The template still governs how the body is structured; filing with `--body-file` applies no template and no label of its own, so both are chosen here.

Titles follow the house style in upstream's existing `enhancement ✨` issues: a plain imperative sentence in sentence case, no conventional-commit prefix, backticks around symbols - "Add go templating object to access current component", "Introduce validation for api.Package".

The order is deliberate. `01` is the cheapest to accept, is backed by upstream's own duplicated code in `zoci.FetchZarfYAML`, and intersects work a maintainer is already doing in [#5421](https://github.com/zarf-dev/zarf/pull/5421) - it is the one that sets the tone for the rest. `04` is the weakest fit of the four: the fat-package shape it serves may simply not be a shape zarf packages take, in which case the signature change reads as generic OCI work rather than zarf work. The draft says so and names the fallback - the `fetchIndex` non-index tolerance and the `FetchIndex` export stand on their own and can be split out if the multi-architecture half is declined.

```sh
gh issue create --repo zarf-dev/zarf --label 'enhancement ✨' \
  --title "Add oci.FetchFile for fetching a layer's bytes by path" \
  --body-file docs/upstream/01-oci-fetchfile.md
```

## The file-staging drafts

These four concern how `zarf package create` turns a component file into a staged file. Each was read against zarf's own code and reproduces with zarf's own artifacts - the collision in `06` is triggered by the shape of the component tarballs inside `ghcr.io/zarf-dev/packages/init`, and `08` uses that same artifact as one of its two worked examples.

| Draft                                                                                        | Title                                                                               | Template             | Label            |
| -------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- | -------------------- | ---------------- |
| [`05-create-redownloads-archive-per-file.md`](05-create-redownloads-archive-per-file.md)     | `zarf package create` downloads and extracts one archive once per file that uses it | `bug_report.md`      | `possible-bug 🐛` |
| [`06-extractpath-collides-with-destination.md`](06-extractpath-collides-with-destination.md) | A file whose `extractPath` begins with its destination's name fails to stage        | `bug_report.md`      | `possible-bug 🐛` |
| [`07-archive-format-by-extension-only.md`](07-archive-format-by-extension-only.md)           | Identify an archive by its content when its name carries no known extension         | `feature_request.md` | `enhancement ✨`  |
| [`08-oci-file-source.md`](08-oci-file-source.md)                                             | Let a component file's `source` name an `oci://` artifact                           | `feature_request.md` | `enhancement ✨`  |

`05` and `06` are the ones to file first: both are defects rather than requests, neither touches an exported signature, and `06` fixes an error message that points away from its own cause. `07` is a small robustness change. `08` is the feature, and it carries the one open design question in the set - whether a second field is the right way to tell two artifact shapes apart - so it wants a conversation before a pull request.

**`05` and `06` share a fix.** Extracting into a directory of its own and copying each member to its destination resolves the collision in `06`, and is also the shape that lets one extraction serve every entry in `05`. They are filed separately because they fail separately, but whoever takes one should read the other.

## Independent PRs, and a different order

Four independent PRs, not a stack. Three of the four touch disjoint files - `04` is `src/pkg/oci/index.go` alone, `03` is `src/pkg/images/` plus a new leaf package, `01` is `src/pkg/oci/fetch.go` - so there is nothing to stack around, and a stack imposes tooling on reviewers who did not ask for it. Four small PRs each land on their own merits; in a stack, one stalled review blocks the rest.

The one real dependency is `01` before `02`: both touch `src/pkg/zoci/fetch.go`, and specifically both touch `FetchZarfYAML`. Land `01` first and `02` gets easier rather than merely conflict-free - after `01` the function is two lines, so "one import is the only blocker" becomes visible in the diff instead of asserted. Sequential, still two separate PRs.

Working order differs from filing order, which is about the conversation rather than the code:

| Order | Work                       | Why here                                                                                                    |
| ----- | -------------------------- | ----------------------------------------------------------------------------------------------------------- |
| 1     | `01` `oci.FetchFile`       | Cheapest, upstream-evidenced, unblocks `02`, intersects [#5421](https://github.com/zarf-dev/zarf/pull/5421) |
| 2     | `03` media-type predicates | Fully independent, mechanical, no design decision to wait on - can go in parallel with `01`                 |
| 3     | `02` zoci importable       | Needs `01` merged *and* a maintainer to pick between the two shapes                                         |
| 4     | `04` index multi-arch      | Largest, weakest fit, most likely to be split - but see the timing section above                            |

Per the same thread, issues come first and PRs follow once the gap and the alignment are agreed - self-assign then, keeping to upstream's contributor guide.

## Why `02` and `04` are held

**`02` - nothing cargoship can reach.** Fixing the internal imports removes one of three independent blockers, and the other two have no issue because neither is fixable from outside. Go links whole packages, so importing `zoci` for any symbol pulls `src/pkg/images` (and with it `docker/cli` and `moby/moby/client`), `packager/layout`, `utils`, `transform` and `types` - disqualifying against the binary budget whatever is called. And `AssembleLayers` takes `[]api.Component` and reads `layout.*` constants, which cargoship has no analogue for. The one genuinely format-agnostic symbol is `LayersFromImages`, and reaching it needs `02`, [#5554](https://github.com/zarf-dev/zarf/issues/5554) *and* the weight problem all solved, so the chain never completes.

That makes `02` the only one of the four with no payoff here at all - which the analysis page already conceded - while its body commits to opening a PR. There is also no deadline to offset that: the `src/api` entry point is purely additive, so it is as fileable in December as it is now. If it goes up later, the cheap version is the same text with the PR offer removed: a plain observation that `src/pkg/` makes a promise the import graph does not keep, left for whoever actually needs `zoci` importable.

**`04` - weakest fit, filed last or not at all.** The reasoning is in the timing section above and in the draft itself. Note the tension: it is the weakest fit *and* the only one of the four that gets harder after v1.0.0, because the `fetchIndex` tolerance is a behaviour change. Filing the issue would cost nothing; writing the PR is what should wait.

## What is deliberately not here

- [`zarf-oci-credential-store.md`](zarf-oci-credential-store.md) - landed upstream in [#5502](https://github.com/zarf-dev/zarf/pull/5502). Nothing to file.
- [`zarf-oci-registry-rewrite.md`](zarf-oci-registry-rewrite.md) - landed upstream in [#5504](https://github.com/zarf-dev/zarf/pull/5504). Nothing to file.
- [`zarf-zoci-reusable-layer-helpers.md`](zarf-zoci-reusable-layer-helpers.md) item 3, exporting the index/manifest recursion on its own - this is the generic-OCI ask the maintainer pre-declined in the thread, and the page already concedes it. Items 1 and 2 do not depend on it.

Two further findings from the file-staging read are real and are deliberately not drafted, because both are generic OCI plumbing rather than anything a zarf package user meets - the category [#5554](https://github.com/zarf-dev/zarf/issues/5554) was closed under:

- **`oci.FetchLayerReader` hands back a reader its caller cannot close.** It wraps the `io.ReadCloser` from `o.Fetch` in a `*content.VerifyReader`, which has no `Close`, so the body leaks. It has no callers anywhere in the repository, which is the only reason it leaks nothing today.
- **`OrasRemote.ResolveRoot` carries an unreachable branch.** `NewOrasRemote` always assigns `targetPlatform: &platform`, so `o.targetPlatform == nil` is never true and its error - "resolved to an image index, but no target platform was specified" - can never be returned. A caller resolving an index without a usable platform gets oras's generic "no matching manifest was found in the manifest list" instead, which names neither the problem nor the fix.

## Currency

Drafts `05` to `08` were read against `main` @ [`1f516c0`](https://github.com/zarf-dev/zarf/commit/1f516c0fce0c), 2026-11-07, which is after v0.89.0. Every line number they cite is permalinked to that commit.

**None of their repro steps has been executed against `zarf` itself.** Each was derived by reading the code at that commit, and each failure was observed on code structurally identical to it rather than by running `zarf package create`. Reproduce before filing: a repro step that does not reproduce costs a maintainer more than the report saves.

Drafts `01` to `04` were read against `main` @ [`1abf721`](https://github.com/zarf-dev/zarf/commit/1abf721), 2026-10-28, not against the vendored release - which is the correction the thread produced, after two of the five pages turned out to be asking for something already on `main`. Re-check before filing if more than a few days pass, and note that v0.89.0 is expected imminently: it carries the two landed items above, so the vendored-copy workarounds for them come out on that bump.
