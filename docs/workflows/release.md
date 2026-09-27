# release.yaml

**Triggers:** push of any tag.

The actual release pipeline ("goreleaser" job), granted `contents: write` (create release, upload archives), `packages: write` (push images), `id-token: write` (OIDC token for cosign), and `attestations: write`.

Sets up Syft (SBOM), cosign, ansible-core, Go, QEMU, and Docker Buildx, then logs into three registries (GHCR, Registry1/DSO, Docker Hub) before running GoReleaser (`goreleaser release --clean`), which builds and publishes binaries, packages, and container images.

Package signing keys (GPG for rpm/deb, a bare RSA key for apk) are written to `RUNNER_TEMP` - outside the checkout, so no `.goreleaser.yaml` glob can accidentally pick them up - and removed at the end regardless of outcome. A missing signing secret disables signing for that artifact type with a warning rather than failing the release.

After GoReleaser runs, the job attests packages, images, and a generated AI-BOM (`.github/ai-bom.json`, stamped with the release version/commit/timestamp) via `actions/attest`, and uploads the resulting provenance bundles to the GitHub release.

A second job, **publish-example**, runs after `goreleaser` completes (only for `v*` tags) and calls `publish-example.yaml` as a reusable workflow with `secrets: inherit`, so a fresh example package gets published against the release that was just cut. Its permissions are written to exactly match what the called workflow needs, since a caller's grant is a ceiling that can only be narrowed downstream.
