# Issue drafts for `zarf-dev/zarf`

The `zarf-oci-*.md` and `zarf-zoci-*.md` pages in this directory are the analysis: what is awkward in `src/pkg/oci` and `src/pkg/zoci`, what was prototyped, and what it measured. The numbered pages here are the issues derived from them, written in upstream's own templates and ready to pass to `gh issue create --body-file`.

The split exists because the two audiences differ. The analysis pages lead with what is awkward for cargoship and close with the case for upstream; the issue drafts are zarf-only and name cargoship nowhere. The bar a maintainer stated in the [OpenSSF thread](https://openssf.slack.com/archives/C07AKUMBDMJ/p1792787963719939) is whether a change provides utility to people interacting with zarf packages and workflows, so an issue that argues from a consumer's needs is arguing the wrong case - the gap has to be visible in zarf's own code, or it is not a gap worth filing. Each draft rests on upstream evidence: `zoci.FetchZarfYAML` bypassing `oci.FetchJSONFile`, `src/pkg/zoci` failing to compile from outside the module, the media-type predicates sitting behind `docker/cli`. Prototype measurements are kept because they describe the patch, not the consumer.


## Timing: v1.0.0, 2026-11-09

Upstream is targeting v1.0.0 for 9 November, and a deprecation policy is being drafted alongside it. That makes *breaking versus additive* the property worth checking before filing anything, because it decides whether a change can be taken whenever someone gets to it or has to make a release:

| Draft                        | Breaking?                            | Consequence                                                                                                                                                                                     |
| ---------------------------- | ------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `01-oci-fetchfile.md`        | No - pure addition                   | Safe to land either side of v1.0.0; no clock on it                                                                                                                                              |
| `02-zoci-unimportable.md`    | Depends which shape maintainers pick | The `src/api` entry point is additive; moving `FetchZarfYAML` removes a symbol. The draft now says so                                                                                           |
| `03-mediatype-predicates.md` | No - file move with re-exports kept  | Safe either side; the re-exports are what make that true                                                                                                                                        |
| `04-index-multi-arch.md`     | Partly - one behaviour change        | `UpdateIndexWithDescriptors` and `FetchIndex` are additive, but making `fetchIndex` tolerate a non-index tag turns an error into a success, which is a behaviour change regardless of signature |

So `04` is the one with an actual deadline, which is the opposite of how it reads: it is the weakest fit of the four *and* the only one that gets harder after v1.0.0. Filing the issue costs nothing and starts the conversation; writing the PR is what can wait.

## The four drafts

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

## Currency

All four drafts were read against `main` @ [`1abf721`](https://github.com/zarf-dev/zarf/commit/1abf721), 2026-10-28, not against the vendored release - which is the correction the thread produced, after two of the five pages turned out to be asking for something already on `main`. Re-check before filing if more than a few days pass, and note that v0.89.0 is expected imminently: it carries the two landed items above, so the vendored-copy workarounds for them come out on that bump.
