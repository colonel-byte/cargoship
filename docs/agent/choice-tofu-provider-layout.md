# Why the OpenTofu provider is a package of this module

The provider that [#302](https://github.com/colonel-byte/cargoship/issues/302) scopes lives at `cmd/terraform-provider-cargoship`, in this repository and in this Go module. Two alternatives were live: its own repository, `colonel-byte/terraform-provider-cargoship`, which is the only layout the OpenTofu registry accepts; and a second Go module here at `tofu/provider`, which would let the provider carry its own tags and keep its dependencies out of the committed vendor tree. This records why neither was taken, and what would make one of them right later.

## The registry constraint, and why it is not decisive

The registry's [provider submission form](https://github.com/opentofu/registry/blob/main/.github/ISSUE_TEMPLATE/provider.yml) validates the repository against `{owner}/terraform-provider-{name}`. That is a rule about the repository name, not about a directory inside it: no tag, package path, or release layout makes a provider under `colonel-byte/cargoship` submittable. Publishing to the registry means moving the code to its own repository, and nothing short of that.

Cargoship exists for airgapped clusters, which is what makes that acceptable. An operator running `tofu init` on a management node with no route to the internet resolves nothing from `registry.opentofu.org`; they reach providers through a `filesystem_mirror` or an `oci_mirror` in their CLI configuration. A listing buys discovery by connected users. It does not change what a disconnected one can install, and the disconnected one is the user this project is for.

## Why one module rather than two

A second module was the first answer, and the argument for it was the vendor tree. `vendor/` is committed and already 167M, because the management node is staged ahead of time and builds with `-mod=vendor`, `GOPROXY=off`. Once this package depends on `terraform-plugin-framework`, that tree grows by **13 modules and about 1,222 files**, and by 28 modules and about 1,998 files once `terraform-plugin-testing` arrives with the acceptance tests. Those numbers are measured rather than guessed: vendor the framework in a throwaway module and diff its `modules.txt` against this one's 435 entries.

What that argument got wrong is what it implied about the CLI. **The linker never loads a package the import graph does not reach**, and nothing here is reachable from `cmd/cargoship`, so the binary an operator installs is byte-for-byte the size it is now whatever this package depends on. The cost is the committed tree and the churn in it -- dependabot re-vendors on every `gomod` bump, so those files land in pull requests that have nothing to do with the provider -- not the artifact anybody ships.

Against that, a second module costs the repository in a way `thirdparty-src/` already documents: *"The module boundary is not honoured by every tool... Anything else added to CI that walks for go.mod files needs the same treatment"* ([docs/dev/thirdparty-src.md](../dev/thirdparty-src.md)). That module is deliberately one that does not build, so every tool only had to be told to leave it alone. A provider module has to build, lint, test and release, so being skipped is its failure mode rather than its exemption: `check-go-mod.yaml` tidies the root module only, `go list ./...` does not cross a module boundary so `Test.Unit` would never see it, `codeql.yaml` names its packages explicitly, `scan-lint.yaml` lints one directory, dependabot watches one `gomod` path, and the pre-commit golangci hooks hand the root module every Go file they are given. Each of those is a small accommodation, and each is one more place where the honest failure is silence.

One module also keeps the release simple. The provider ships on the CLI's `v*` tags, so there is no component tag, no `separate-pull-requests`, no `exclude-paths`, and no second `.goreleaser.yaml` -- `release-please-config.json` is untouched. The `colonel_byte.cargoship` Ansible collection already works this way, bumped in lockstep with the CLI through `extra-files` on `galaxy.yml`, so a second artifact riding the CLI's version is the established pattern here rather than a new one.

What is given up is independent versioning: a provider patch means a CLI release and the reverse, and the provider's version is the CLI's version. That is the cost to watch, and the first trigger for revisiting below.

## How it gets distributed: ghcr.io as an OCI mirror

The registry listing is not the only way to hand somebody a provider, and for this project it is not the best one. OpenTofu can install a provider from an **OCI registry used as a provider mirror**, configured in the CLI configuration:

```hcl
provider_installation {
  oci_mirror {
    repository_template = "ghcr.io/colonel-byte/tofu-providers/${namespace}/${type}"
    include             = ["registry.opentofu.org/colonel-byte/*"]
  }
}
```

This matters for three reasons. The repository name is unconstrained, so `{owner}/terraform-provider-{name}` never enters into it. The artifact wants no GPG signature and no `SHA256SUMS` -- an image index with `artifactType: application/vnd.opentofu.provider`, a per-platform manifest each with `artifactType: application/vnd.opentofu.provider-target` and a `platform`, and one layer of media type `archive/zip` holding the same `.zip` the registry protocol would serve -- while `.goreleaser.yaml` signs with cosign, which the [registry protocol](https://opentofu.org/docs/internals/provider-registry-protocol/) does not accept. And credentials come from the OCI ecosystem's own locations, the ones `docker login` already writes, so an operator who can pull cargoship's images can install its provider.

It also reuses what this repository already does. `.goreleaser.yaml` pushes four image variants to ghcr.io on every release, so the registry, the authentication and the publish step all exist; the provider adds an artifact to a registry already in use rather than a new distribution channel. Tags are the version in SemVer 2.0.0 form, with `_` in place of `+` for build metadata, and ORAS 1.3.0 or newer is the tooling for assembling the index.

Two limits worth stating plainly. OpenTofu does not yet support an OCI registry as the **primary** installation source for a provider, so a mirror is a redirection of a source address rather than a source address of its own: the configuration still names `registry.opentofu.org/colonel-byte/cargoship` (or whatever hostname is chosen), and the `oci_mirror` block is what sends the request to ghcr.io instead. And an airgapped operator still needs either a reachable internal OCI registry or a `filesystem_mirror`, exactly as they do for images today.

So the publishing plan is: ghcr.io as an OCI mirror first, since it needs no listing and no second signing path, and a registry listing only if connected users turn out to want discovery. That ordering is why the GPG work stays deferred rather than being a prerequisite.

The pipeline for it is `mage release:tofuProvider` (`magefiles/pkg/release`), run by [`release-tofu-provider.yaml`](../workflows/release-tofu-provider.md) on the same `v*` tag the CLI release fires on. It is a mage target rather than another `builds:` entry in `.goreleaser.yaml` because what has to be published is not a release asset: it is an OCI image index assembled to the shape above, and goreleaser has nothing to say about composing one. Building the zips there too keeps the artifact in one place and lets them carry a fixed entry timestamp, so the same commit produces the same bytes -- which `zip(1)` does not. `mage release:tofuProviderLayout` runs everything but the copy to the registry and leaves the layout under `build/`, so the artifact shape can be checked without pushing anything.

## What the schema has to carry because of this

One piece of the provider's schema cannot wait for the provider, because an attribute that does not exist cannot be compared later. [#307](https://github.com/colonel-byte/cargoship/issues/307) wants a lowered engine version refused at plan time rather than mid-apply, and the input that check needs is the **last-known running version of each host**, held as a computed per-host attribute sourced from `h.Metadata.DistroVersion` (`pkg/phase/12_gather_facts_distro.go`). With it in state, a downgrade is a plan error: no SSH, no host touched, and the operator sees it before approving anything. Without it, the provider can only forward the CLI's refusal, which happens after a run has started.

The CLI keeps its own refusal as the backstop, for the case with no state file at all, and it now fires before any phase writes to a host.

## When to revisit

Two triggers, pointing at different answers.

**Independent versioning starts to matter** -- the provider needs a patch the CLI does not, or the reverse, often enough to be worth paying for. That is the second module at `tofu/provider`, with a release-please component, `exclude-paths` on the root package, `separate-pull-requests`, a `v*` narrowing on `release.yaml`'s tag trigger so a component tag cannot cut a CLI release, and the six CI accommodations listed above. Everything in this document except the package path survives that move.

**A registry listing is actually wanted**, because enough connected users resolve providers through `registry.opentofu.org` for discovery to matter. That is the separate repository, and it is the one move that cannot be undone cheaply from inside this one. It is still not a rewrite: nobody imports a provider as a library, so the package path changing breaks no consumer, and `git subtree split` carries the history across. What it adds is a GPG signing path and a goreleaser configuration the OCI mirror does not need.
