# Why `pkg/signing` stopped being a wrapper around cosign's CLI

Cargoship signs and verifies the `distro.yaml` blob with cosign, and until this change it did so through `github.com/zarf-dev/zarf/src/pkg/signing`. That package is a thin wrapper over cosign's CLI command structs, which is exactly what cargoship wants, so importing it rather than reimplementing it was the right call. The reason it is now a fork is not a disagreement about the wrapper. It is five blank imports' worth of binary, and then the command structs behind them.

## What the import cost

`zarf/src/pkg/signing` registers four cloud KMS plugins and cosign's ambient OIDC credential providers by side effect:

```go
_ "github.com/sigstore/cosign/v3/pkg/providers/all"
_ "github.com/sigstore/sigstore/pkg/signature/kms/aws"
_ "github.com/sigstore/sigstore/pkg/signature/kms/azure"
_ "github.com/sigstore/sigstore/pkg/signature/kms/gcp"
_ "github.com/sigstore/sigstore/pkg/signature/kms/hashivault"
```

Every one of those resolves a key by reaching a cloud endpoint - KMS for the key material, Fulcio and an OIDC issuer for the keyless flow. The management node cargoship runs from is air-gapped, so none of them can be used from where cargoship actually runs. `aws-sdk-go-v2/service/kms` and its three siblings were reachable from nothing else in the tree. Removing them measured **100.39MiB to 92.98MiB, -7.41MiB**, with `mage build:binary`.

Go has no way to drop a transitive blank import. The package that writes `_ "…/kms/aws"` registers it for every binary that links the package, however the linker is invoked. Dropping the registration means not linking that package, which means owning a copy.

## The second step: the CLI structs went too

Dropping the KMS registrations left the cosign CLI command structs in place, and they carry their own weight: `cmd/cosign/cli/verify` pulls Open Policy Agent through `pkg/cosign/rego` and cuelang through `pkg/cosign/cue`, both for policy evaluation cargoship never used, and `cmd/cosign/cli/options` pulls go-github and the GitLab client through `pkg/cosign/signature` for git-based key storage cargoship never used.

Taking them out, with the keyless surfaces that needed them, measured **87.11MiB to 56.79MiB, -30.32MiB**. Signing and verification are now `sigstore-go`, in `signkey.go` and `verifykey.go`: `sign.Bundle` with a `Keypair` adapter over a cosign-format private key, and `verify.NewVerifier` with `WithKey()` and `WithNoObserverTimestamps()`. The bundle format is unchanged, and the interoperability was tested in both directions - a cargoship-signed bundle verifies under `cosign verify-blob --key`, and a `cosign sign-blob` bundle verifies under cargoship. `TestSignBlobWithKeyRoundTrip` and `TestVerifyBlobWithKeyAgainstCosignCLI` pin both.

One seam onto cosign's library is kept deliberately: `cosign.LoadPrivateKey` reads the encrypted private-key envelope, which is cosign's own scrypt and secretbox format. Re-deriving it here would be a way to get it subtly wrong, and the cost of keeping it is `sigstore/rekor` and `goleveldb` arriving through `cosign/pkg/cosign` - about 1.3MiB, which is worth paying for not reimplementing a key-decryption format.

## What no longer works

Keyless (Fulcio/OIDC) signing and verification, and cloud KMS key references (`awskms://`, `azurekms://`, `gcpkms://`, `hashivault://`). All of them need network egress to a cloud the air-gapped management node cannot reach. A `COSIGN_PASSWORD`-protected local keypair is the only signing method, and is what the e2e suite exercises.

Whether keyless should return, and in what form, is [#848](https://github.com/colonel-byte/cargoship/issues/848). `sigstore-go/pkg/verify` supports `WithCertificateIdentity`, so keyless verification is reachable without relinking the CLI structs - which is the option that issue argues for.

A package already signed keylessly cannot be verified. `VerifyPackage` detects that from the bundle via `ReadBundleInfo` and fails naming the method and the issue, rather than letting the key verifier report a mismatch it cannot explain.

## What was dropped as unused

`SignManifest` and `VerifyManifest`, their options structs and defaults. Cargoship signs the `distro.yaml` blob, not an OCI manifest referrer, and nothing in the tree called them. They were the only users of `go-containerregistry/pkg/v1/remote`, `pkg/name` and `zarf/src/types` inside the package, so those imports went with them.

## A build tag would be the better fix, upstream

The KMS half of this did not have to be a fork. A `//go:build cloud` file in zarf's own `src/pkg/signing`, holding nothing but those five blank imports, would gate the registrations on a tag the consumer chooses:

```go
//go:build cloud

package signing

import (
	_ "github.com/sigstore/cosign/v3/pkg/providers/all"
	_ "github.com/sigstore/sigstore/pkg/signature/kms/aws"
	// ...
)
```

Measured on a throwaway module importing exactly those packages, release flags, `linux/amd64`: **4.58MiB without the tag against 23.07MiB with it.** The linker never sees the KMS providers unless somebody asks for them, and `go build -tags cloud` restores the capability for a consumer that can reach a cloud. That is strictly better than a fork, because it tracks upstream for free.

Two things it does not do, which is why the fork is still here.

**It does not shrink `vendor/`.** `go mod tidy` cannot know which tags a consumer will set, so it keeps the requirement for every build configuration, and `go mod vendor` copies the SDKs in. The same throwaway module vendors 34MiB including a full `aws-sdk-go-v2` tree with the tag unset. The binary is the win; the dependency surface is not.

**It does not touch the second half of the fork.** The cosign CLI command structs are called directly -- `verify.VerifyBlobCmd`, `sign.SignBlobCmd` -- not blank-imported, so no tag excludes them, and they are what pull Open Policy Agent, cuelang, go-github and the GitLab client. Cargoship does not want those code paths at any build setting: keyless is removed and the rego and cue policy evaluation was never used.

If this is raised upstream, the polarity matters: the tag has to be **opt-in** (`cloud` adds the providers) rather than opt-out, or every consumer stays fat unless they know to disable something. The honest argument against it is that a consumer who forgets the tag loses `awskms://` support at runtime rather than at build time, so the "unsupported provider" error needs to name the tag.

## Do not revert this to the upstream import

The fork looks like duplication, and from inside `pkg/signing` there is nothing to show what reverting would cost: the option structs have the same field names, and `CosignSignBlobWithOptions` and `CosignVerifyBlobWithOptions` have the same signatures they always had. Going back to `zarf/src/pkg/signing` relinks four cloud SDKs and the whole cosign CLI tree -- **37.73MiB across the two steps**, on a binary that is now 39.48MiB -- none of it reachable from an air-gapped host.

If the fork needs resyncing with upstream zarf, resync `bundle.go` and leave the rest alone. `cosign.go` is no longer zarf's code in any meaningful sense: the option structs are cargoship's, and the bodies delegate to `signkey.go` and `verifykey.go` rather than to cosign's commands. The attribution header stays because the structs and `CheckOverwrite` began as zarf's.
