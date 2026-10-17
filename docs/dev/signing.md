# Signing, and the embedded trusted root

Cargoship signs the `distro.yaml` blob in a package and verifies it on pull, install and verify. The wrapper around cosign lives in [`pkg/signing`](https://github.com/colonel-byte/cargoship/tree/main/pkg/signing), a fork of zarf's package of the same name - [`docs/agent/choice-signing-fork.md`](../agent/choice-signing-fork.md) records why it is a fork and what it deliberately leaves out.

The supported path is a local cosign keypair whose password comes from `COSIGN_PASSWORD`. Cloud KMS key references and ambient-OIDC keyless signing are not available: all of them need to reach a cloud endpoint, and the management node is air-gapped.

```sh
cosign generate-key-pair                                     # cosign.key, cosign.pub
COSIGN_PASSWORD=... cargoship sign ./my-package --signing-key cosign.key
cargoship verify ./my-package --key cosign.pub
```

The end-to-end coverage is in `test/e2e/noncluster`, and it does real signing against a real generated keypair rather than mocking cosign:

```sh
go test -run 'TestCargoshipSign|TestCargoshipSignedRoundTrip|TestCargoshipVerifyCommand' ./test/e2e/noncluster/
```

## The embedded trusted root is a pin

`pkg/signing/embedded_trusted_root.json` is the Sigstore TrustedRoot, embedded into the binary so a keyless verification that supplies no `--trusted-root` has one to fall back on. It names Fulcio, Rekor, the CT logs and the timestamp authority, each with key material and validity windows, so it goes stale on sigstore's schedule rather than ours.

No updater owns it. Dependabot parses no format it is written in, and reconstructing it with `cosign trusted-root create` means supplying every service's certificate chain and public key by hand, which is not a refresh so much as a rebuild. So this is the third answer the pin rule allows - see [`magefiles/AGENTS.md`](https://github.com/colonel-byte/cargoship/blob/main/magefiles/AGENTS.md#every-pin-needs-an-owner) - a written manual bump rather than automation.

Refresh it from sigstore's TUF repository, which is where cosign itself gets it. Use a throwaway `TUF_ROOT` so an operator's own `~/.sigstore` is left alone:

```sh
TUF_ROOT=$(mktemp -d) sh -c 'cosign initialize && cp "$TUF_ROOT/tuf-repo-cdn.sigstore.dev/targets/trusted_root.json" \
  pkg/signing/embedded_trusted_root.json'
```

Then check that it changed for a reason, and that the result still verifies:

```sh
git diff --stat pkg/signing/embedded_trusted_root.json
go test -run 'TestCargoshipSign|TestCargoshipSignedRoundTrip|TestCargoshipVerifyCommand' ./test/e2e/noncluster/
```

An empty diff is the normal outcome and means the pin was already current - at the time of writing it is byte-identical to upstream. A diff that drops a `tlogs` or `certificateAuthorities` entry rather than adding one is worth reading before committing: it narrows what the binary will accept, which is sometimes the point of the upstream change and sometimes a sign the fetch was incomplete.
