# release-tofu-provider.yaml

**Triggers:** push of a `v*` tag, or `workflow_dispatch` with a version.

Publishes the OpenTofu provider to `ghcr.io` as a provider mirror artifact. The provider is a package of this module, so it ships on the CLI's version: this fires on the same `v*` tag [release.yaml](release.md) does. It is a separate workflow rather than another job there because it shares nothing with the goreleaser run - no signing keys, no GitHub Release, no attestations - and a provider that failed to publish should not read as a failed CLI release.

One job, **publish**, granted `packages: write` on top of the top-level `contents: read`. Nothing else is needed: an OCI mirror takes no GitHub Release, no `SHA256SUMS` and no GPG signature, which is most of why it is the distribution path rather than a registry listing. See [choice-tofu-provider-layout](../agent/choice-tofu-provider-layout.md).

It sets up Go, logs into `ghcr.io` with the workflow token, reads the version out of the tag with the leading `v` removed, and runs `go run ./magefiles/core release:tofuProvider`. The version reaches the target through `env` rather than being interpolated into the `run` block.

That target cross-compiles the provider for each published platform, zips each binary as `terraform-provider-cargoship_v<version>` with a fixed entry timestamp so the same commit produces the same bytes, generates one manifest per platform with artifactType `application/vnd.opentofu.provider-target` and the target platform, assembles an index with artifactType `application/vnd.opentofu.provider`, and copies the index to the repository using the oras-go library. `mage release:tofuProviderLayout <version>` does everything but the copy and leaves the OCI layout under `build/`, which is how the artifact shape is checked without a registry. See [mage](../dev/mage.md).

An operator installs the result through a `provider_installation` block in their CLI configuration:

```hcl
provider_installation {
  oci_mirror {
    repository_template = "ghcr.io/colonel-byte/tofu-providers/${namespace}/${type}"
    include             = ["registry.opentofu.org/colonel-byte/*"]
  }
}
```
