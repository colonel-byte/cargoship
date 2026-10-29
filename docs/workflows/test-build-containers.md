# test-build-containers.yaml

**Triggers:** pull requests (opened/reopened/synchronize/labeled) touching `containers/**`, `.goreleaser.yaml`, `.release-please-manifest.json`, or this workflow file - gated on the PR carrying the `docker` label.

Actually builds all four container images (`ansible`, `base`, `deb`, `ubi`) via `goreleaser release --snapshot --clean --skip=sign,sbom`, closing a gap `scan-lint.yaml`'s hadolint job (syntax only) does not cover: a Dependabot bump that breaks the actual build wasn't otherwise caught until release.

These Dockerfiles are written for GoReleaser's `dockers_v2` pipe, not a plain `docker build` - several `COPY` nfpm-produced `.rpm`/`.deb` packages and extra files that only GoReleaser stages. That pipe has no per-image build flag, so a snapshot build always builds all four images together; there's no cheaper way to build "just the one that changed."

`labeled` is included in `types` because Dependabot's `docker` label is applied *after* the PR opens - without it, the `if` condition would be evaluated (and fail) on the `opened` event before the label exists, and nothing would re-trigger once it's added.

A final step installs the two release packages together in a throwaway container - `cargoship` and the `noarch` `ansible-collection-colonel-byte-cargoship` - and asserts that `dnf` resolves the dependency between them and that the collection lands on ansible-core's default path. That check belongs in the `cargoship-ansible` image build and cannot live there: a `noarch` nfpm artifact reaches no `dockers_v2` build context, for the reasons in [goreleaser](../dev/goreleaser.md#the-collection-package). The container's base is read off `containers/ansible/Dockerfile`'s `FROM` line rather than named again, so Dependabot keeps owning the one pin.

`--snapshot` skips publish/validate/announce entirely - nothing is pushed anywhere.
