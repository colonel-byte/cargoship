# Why `pkg/signing` stopped being a wrapper around cosign's CLI

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

## The second step: the CLI structs went too

Dropping the KMS registrations left the cosign CLI command structs in place, and they carry their own weight: `cmd/cosign/cli/verify` pulls Open Policy Agent through `pkg/cosign/rego` and cuelang through `pkg/cosign/cue`, both for policy evaluation cargoship never used, and `cmd/cosign/cli/options` pulls go-github and the GitLab client through `pkg/cosign/signature` for git-based key storage cargoship never used. Another 16 MB.

Signing and verification are now `sigstore-go`, in `signkey.go` and `verifykey.go`: `sign.Bundle` with a `Keypair` adapter over a cosign-format private key, and `verify.NewVerifier` with `WithKey()` and `WithNoObserverTimestamps()`. The bundle format is unchanged, and the interoperability was tested in both directions - a cargoship-signed bundle verifies under `cosign verify-blob --key`, and a `cosign sign-blob` bundle verifies under cargoship. `TestSignBlobWithKeyRoundTrip` and `TestVerifyBlobWithKeyAgainstCosignCLI` pin both.

One seam onto cosign's library is kept deliberately: `cosign.LoadPrivateKey` reads the encrypted private-key envelope, which is cosign's own scrypt and secretbox format. Re-deriving it here would be a way to get it subtly wrong, and the cost of keeping it is `sigstore/rekor` and `goleveldb` arriving through `cosign/pkg/cosign` - about 1.3 MB, which is worth paying for not reimplementing a key-decryption format.

## What no longer works

Keyless (Fulcio/OIDC) signing and verification, and cloud KMS key references (`awskms://`, `azurekms://`, `gcpkms://`, `hashivault://`). All of them need network egress to a cloud the air-gapped management node cannot reach. A `COSIGN_PASSWORD`-protected local keypair is the only signing method, and is what the e2e suite exercises.

Whether keyless should return, and in what form, is [#848](https://github.com/colonel-byte/cargoship/issues/848). `sigstore-go/pkg/verify` supports `WithCertificateIdentity`, so keyless verification is reachable without relinking the CLI structs - which is the option that issue argues for.

A package already signed keylessly cannot be verified. `VerifyPackage` detects that from the bundle via `ReadBundleInfo` and fails naming the method and the issue, rather than letting the key verifier report a mismatch it cannot explain.

## What was dropped as unused

`SignManifest` and `VerifyManifest`, their options structs and defaults. Cargoship signs the `distro.yaml` blob, not an OCI manifest referrer, and nothing in the tree called them. They were the only users of `go-containerregistry/pkg/v1/remote`, `pkg/name` and `zarf/src/types` inside the package, so those imports went with them.

## Do not revert this to the upstream import

The fork looks like duplication, and from inside `pkg/signing` there is nothing to show that reverting to `zarf/src/pkg/signing` would cost anything - the API is identical and the bodies are the same. It costs 11MB and relinks four cloud SDKs that cannot be called from an air-gapped host. If the fork needs to be resynced with upstream zarf, resync the bodies and leave the import block alone.

The remaining weight behind the cosign CLI structs is larger still and is tracked separately: `cmd/cosign/cli/verify` pulls Open Policy Agent (9.6MB, via `pkg/cosign/rego`) and cuelang (3.8MB, via `pkg/cosign/cue`) for policy verification cargoship does not use, and `cmd/cosign/cli/options` pulls go-github, the GitLab client and the AWS core SDK through `cosign/pkg/signature`. Replacing the CLI structs with `sigstore-go`'s bundle verification would cut roughly another 45MB, but it is a rewrite of code sitting on a trust boundary and is not a refactor to do casually. Measured sizes and the comparison binaries are in the issue.
