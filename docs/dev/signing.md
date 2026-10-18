# Signing, and the embedded trusted root

Cargoship signs the `distro.yaml` blob in a package and verifies it on pull, install and verify. The wrapper around cosign lives in [`pkg/signing`](https://github.com/colonel-byte/cargoship/tree/main/pkg/signing), a fork of zarf's package of the same name - [`docs/agent/choice-signing-fork.md`](../agent/choice-signing-fork.md) records why it is a fork and what it deliberately leaves out.

A local cosign keypair whose password comes from `COSIGN_PASSWORD` is the only supported signing method. Cloud KMS key references and keyless (Fulcio/OIDC) signing and verification are not available: all of them need to reach a cloud endpoint, and the management node is air-gapped. Whether keyless should return is [#848](https://github.com/colonel-byte/cargoship/issues/848).

```sh
cosign generate-key-pair                                     # cosign.key, cosign.pub
COSIGN_PASSWORD=... cargoship sign ./my-package --signing-key cosign.key
cargoship verify ./my-package --key cosign.pub
```

The end-to-end coverage is in `test/e2e/noncluster`, and it does real signing against a real generated keypair rather than mocking cosign:

```sh
go test -run 'TestCargoshipSign|TestCargoshipSignedRoundTrip|TestCargoshipVerifyCommand' ./test/e2e/noncluster/
```

## There is no embedded trusted root any more

An earlier revision embedded `pkg/signing/embedded_trusted_root.json`, the Sigstore TrustedRoot, so a keyless verification that supplied no `--trusted-root` had one to fall back on. Keyless verification is gone and nothing key-based reads a trusted root, so the file and its manual refresh procedure went with it. That is one fewer pin with no automatic owner - see [`magefiles/AGENTS.md`](https://github.com/colonel-byte/cargoship/blob/main/magefiles/AGENTS.md#every-pin-needs-an-owner) for why that matters.

If keyless verification returns through [#848](https://github.com/colonel-byte/cargoship/issues/848), the trusted root comes back with it, and so does the obligation to refresh it. The procedure that worked was:

```sh
TUF_ROOT=$(mktemp -d) sh -c 'cosign initialize && cp "$TUF_ROOT/tuf-repo-cdn.sigstore.dev/targets/trusted_root.json" <destination>'
```
