# Why `pkg/signing` is a fork of zarf's, and what was left out

Cargoship signs and verifies the `distro.yaml` blob with cosign, and until this change it did so through `github.com/zarf-dev/zarf/src/pkg/signing`. That package is a thin wrapper over cosign's CLI command structs, which is exactly what cargoship wants, so importing it rather than reimplementing it was the right call. The reason it is now a fork is not a disagreement about the wrapper. It is eight blank imports' worth of binary.

## What the import cost

`zarf/src/pkg/signing` registers four cloud KMS plugins and cosign's ambient OIDC credential providers by side effect:

```go
_ "github.com/sigstore/cosign/v3/pkg/providers/all"
_ "github.com/sigstore/sigstore/pkg/signature/kms/aws"
_ "github.com/sigstore/sigstore/pkg/signature/kms/azure"
_ "github.com/sigstore/sigstore/pkg/signature/kms/gcp"
_ "github.com/sigstore/sigstore/pkg/signature/kms/hashivault"
```

Every one of those resolves a key by reaching a cloud endpoint - KMS for the key material, Fulcio and an OIDC issuer for the keyless flow. The management node cargoship runs from is air-gapped, so none of them can be used from where cargoship actually runs. `aws-sdk-go-v2/service/kms` and its three siblings were reachable from nothing else in the tree, and they were 11MB of a 150MB binary.

Go has no way to drop a transitive blank import. The package that writes `_ "…/kms/aws"` registers it for every binary that links the package, however the linker is invoked. Dropping the registration means not linking that package, which means owning a copy.

## What the fork keeps

Everything cargoship called, unchanged: `SignBlobOptions`, `VerifyBlobOptions`, their `Default…` constructors, `CheckOverwrite`, `CosignSignBlobWithOptions`, `CosignVerifyBlobWithOptions`, and `ReadBundleInfo` with `SigningMethodKey` / `SigningMethodKeyless`. The bodies are zarf's, still delegating to `verify.VerifyBlobCmd` and `sign.SignBlobCmd`, so the verification semantics are cosign's and were not rewritten.

Key-based signing and key-based verification are the hard requirement - a cargoship release is verified with `cosign verify-blob --key cosign.pub --bundle …`, and cargoship's own packages carry a `distro.bundle.sig`. Keyless *verification* also still works: it is `cmd/cosign/cli/verify` doing the work, and the embedded trusted root is still there for a verify that supplies no `--trusted-root`.

What no longer works is signing or verifying against a cloud KMS key reference (`awskms://`, `azurekms://`, `gcpkms://`, `hashivault://`), and keyless signing that obtains its OIDC token from an ambient provider. Both need network egress to a cloud the air-gapped node cannot reach. A `COSIGN_PASSWORD`-protected local keypair is the supported path and is what the e2e suite exercises.

## What was dropped as unused

`SignManifest` and `VerifyManifest`, their options structs and defaults. Cargoship signs the `distro.yaml` blob, not an OCI manifest referrer, and nothing in the tree called them. They were the only users of `go-containerregistry/pkg/v1/remote`, `pkg/name` and `zarf/src/types` inside the package, so those imports went with them.

## Do not revert this to the upstream import

The fork looks like duplication, and from inside `pkg/signing` there is nothing to show that reverting to `zarf/src/pkg/signing` would cost anything - the API is identical and the bodies are the same. It costs 11MB and relinks four cloud SDKs that cannot be called from an air-gapped host. If the fork needs to be resynced with upstream zarf, resync the bodies and leave the import block alone.

The remaining weight behind the cosign CLI structs is larger still and is tracked separately: `cmd/cosign/cli/verify` pulls Open Policy Agent (9.6MB, via `pkg/cosign/rego`) and cuelang (3.8MB, via `pkg/cosign/cue`) for policy verification cargoship does not use, and `cmd/cosign/cli/options` pulls go-github, the GitLab client and the AWS core SDK through `cosign/pkg/signature`. Replacing the CLI structs with `sigstore-go`'s bundle verification would cut roughly another 45MB, but it is a rewrite of code sitting on a trust boundary and is not a refactor to do casually. Measured sizes and the comparison binaries are in the issue.
