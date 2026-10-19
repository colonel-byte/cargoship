# Removing the OpenTofu provider, once it lives in its own repository

This is the procedure for taking the provider out of this repository after it has moved to `colonel-byte/terraform-provider-cargoship`. It is written ahead of the move rather than after it, because the one thing a removal of this shape gets wrong is deleting a little too much: most of what carries `tofu` in its name belongs to the provider, and a handful of things that look like they do are load-bearing for the CLI.

[choice-tofu-provider-layout](choice-tofu-provider-layout.md) is the companion piece and should be read first: it records why the provider is a package of this module today, and the conditions under which moving it becomes right. Nothing here argues for the move. [choice-zarf-collection-removal](choice-zarf-collection-removal.md) is the precedent for how the last one went, and the shape this file should take afterwards - a record of where the code went, kept at this name so published links still resolve.

## Before deleting anything

The move itself is `git subtree split` on the paths below, into a repository named `{owner}/terraform-provider-{name}` - the registry validates that name and no tag or directory layout substitutes for it. Do that first and confirm the new repository builds and releases, because the history is much easier to carry across while it is still here.

Two things to settle with a person rather than from the code:

- **Whether the provider keeps resolving through this repository's OCI mirror** during a transition. If it does, the release target below stays until the new repository's registry listing works, and this removal is two changes rather than one.
- **What happens to `pkg/action.NewRefresh`.** It exists for the provider: a read-only run used during a plan. After the move its only callers here are the phase-reference generator and its own tests, so it is either kept as a documented action the CLI does not expose, or it goes with the provider and `docs/phases/refresh.md` goes with it. Both are defensible; deleting it quietly is not.

## What is the provider's, and goes

| Path                                         | What it is                                                                                |
| :------------------------------------------- | :---------------------------------------------------------------------------------------- |
| `cmd/terraform-provider-cargoship/`          | the released binary's `main`                                                              |
| `internal/tofuprovider/`                     | the schema, the resource, the data source, the converger                                  |
| `test/e2e/tofu/`                             | the provider e2e suite, including its microvm backend                                     |
| `example/terragrunt/`                        | the shipped module and the Terragrunt layout the suite consumes                           |
| `docs/tofu/`                                 | the generated provider reference, one of the trees [`docs/AGENTS.md`](../AGENTS.md) lists |
| `magefiles/pkg/release/tofuprovider.go`      | the OCI mirror build and push                                                             |
| `magefiles/pkg/gen/docs/tofu.go`             | the generator that writes `docs/tofu/`                                                    |
| `docs/agent/choice-tofu-provider-layout.md`  | why it was a package of this module                                                       |
| `docs/agent/choice-tofu-secrets.md`          | why the kubeconfig is withheld unless asked for                                           |
| `docs/agent/choice-tofu-host-removal.md`     | why a removal is a marker rather than a deletion                                          |
| `docs/agent/choice-tofu-e2e.md`              | why the suite drives the shipped module                                                   |
| `docs/agent/design-tofu-provider-install.md` | the install path's design notes                                                           |
| `docs/dev/tofu-provider.md`                  | the development loop                                                                      |
| `docs/dev/e2e-tofu-tests.md`                 | how to run the suite                                                                      |
| `docs/guides/opentofu-terragrunt.md`         | the practitioner guide                                                                    |

The four `choice-tofu-*` records should be published in the new repository before they are deleted here, the way the zarf records were, and this file should then link to them at their new addresses. A decision record that simply disappears costs more than the page it occupied.

## What is shared, and is edited rather than deleted

- **`magefiles/release.go`** - drop `Release.TofuProvider`, `Release.TofuProviderDev` and `Release.TofuProviderLayout`. The `Release` namespace holds nothing else, so it goes with them; see [mage](../dev/mage.md), which documents the namespace as existing for the provider alone.
- **`magefiles/test.go`** - drop `Test.EndToEndTofu` and `Test.EndToEndTofuVM`.
- **`magefiles/pkg/testrunner/testrunner.go`** - drop `RunE2ETofu` and `TofuProviderVersion`.
- **`magefiles/pkg/gen/docs/`** - drop the `docs/tofu` call from `Document()`, and the `refresh` entry from `phaseDocs()` if `NewRefresh` goes too.
- **`.gitattributes`** - drop `docs/tofu/**/*.md linguist-generated`. Per [the rule in `AGENTS.md`](../../AGENTS.md), a pattern matching nothing is not an error anybody sees, so this is easy to leave behind.
- **`README.md`** and **`docs/SUMMARY.md`** - the README is hand-written and will not correct itself; `SUMMARY.md` and `docs/index.md` are generated, so `go run ./magefiles/core generate:document` fixes them once the README is right.
- **`cmd/cargoship/deps_test.go`** - see below. This one is a decision, not a deletion.
- **`go.mod` / `go.sum` / `vendor/`** - `go mod tidy && go mod vendor`. `go mod vendor` deletes the four generated `osv-scanner.toml` files under `vendor/`; restore them, per the records in this directory.

## What looks like the provider's and is not

- **`cmd/cargoship/deps_test.go`** asserts that `internal/tofuprovider` and `hashicorp/terraform-plugin` are **not** linked into the CLI binary. Today that holds: `go list -deps ./cmd/cargoship` reports zero such packages. The test becomes vacuous when the provider leaves, but what it protects does not - it is the guard that keeps a provider-shaped dependency out of an air-gapped binary, and the [binary size budget](../../AGENTS.md) is why that matters. Keep the file and its other entries; drop only the two lines that name packages no longer in the module.
- **`api/colonel-byte.dev/v1alpha1/cluster.Host.ConfiguredAddress`** was added for the provider's `address` attribute and is now also what the run receipt identifies a host by. It stays.
- **`pkg/action.KubeConfigOptions.NoWrite`, `KubeConfig.Bytes` and `KubeConfig.Config`** were added so the provider could have the credentials without writing `~/.kube/config`. `cargoship kube-config --no-write` is not the same thing, so check the CLI's own callers before assuming these go.
- **`zarf-dev/zarf`** in `go.mod` has nothing to do with any of this. It is a library dependency for package formats, architecture detection and OCI handling, and predates both the provider and the collection that did leave. The zarf removal record exists partly to stop exactly this mistake being made twice.
- **`pkg/phase` and `pkg/action`** generally. The provider drives the same phases the CLI does; that is the whole premise. Nothing under `pkg/` is the provider's except the three members named above.

## Afterwards

Rewrite this page as the record of where the provider went, following [choice-zarf-collection-removal](choice-zarf-collection-removal.md): keep the file at this name, because every page under `docs/` is published and a rename costs an `[output.html.redirect]` entry in [`book.toml`](../../book.toml) that has to be kept for as long as anything outside the repository might hold the old link - see [`docs/AGENTS.md`](../AGENTS.md), say which repository and which release carries it now, link the decision records at their new addresses, and say plainly what was *not* lost - a reader who finds `tofu` in `go.mod` and no provider should not have to work that out.

Then measure the binary. The provider's dependencies are vendored for the provider and linked only into its own binary, so `mage build:binary` should not move much - but "should not" is not a measurement, and [build-flags](../dev/build-flags.md) is the only safe way to produce a number that can be compared with the one before the removal.
