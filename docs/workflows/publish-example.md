# publish-example.yaml

**Triggers:** manual dispatch (choose a flavor or "changed"), `workflow_call` (from `release.yaml` after a release), pull requests carrying the `publish-example` label.

Builds the example distro definitions shipped in this repo using the **released** cargoship CLI (not one built in this run) and publishes them to GHCR - exercising what a user actually gets from a release, against the definitions the repo ships.

**Two jobs:**

- **select** - decides what to publish before building anything (cheap API probes vs. expensive multi-gigabyte builds). "Changed" mode checks every flavor's newest example version against what's already in the registry and only selects ones that moved. A named flavor is always published regardless of registry state. Output is grouped into one matrix entry per distro (RKE2 vs. k3s), because flavors of the same distro/version share most of their cached image pulls.
- **publish-example** - for each distro, builds and publishes its selected flavors sequentially in one job, so the `~/.cargoship-cache` bulk (image lists, RPMs) is shared across flavors instead of pulled once per flavor. Each flavor is published under two tags: the regular version tag and a precise `<version>-<job>-<short-sha>` tag. Signing uses `--signing-key env://COSIGN_PRIVATE_KEY` (key material never touches disk).

On a pull request, publishing goes to a per-PR namespace (`cargoship-examples/pr-N`) instead of the real reference, so test runs of the workflow itself never clobber a release artifact. `prune-example-pr-packages.yaml` cleans those up later. Fork PRs are excluded since they lack the signing/write secrets needed to publish.

The chain from a release is: release-please cuts the release and pushes a tag → the tag push triggers `release.yaml`'s goreleaser job → that job calls this workflow via `needs`, ensuring the release archives exist before `setup-cargoship` tries to install the just-released CLI.
