# Security Policy

## Supported versions

Cargoship is pre-1.0 and releases from `main` on every conventional commit that warrants one, so fixes land in a new minor release rather than being backported. Only the most recent release line receives security fixes; upgrade before reporting an issue against an older tag.

| Version | Supported |
| ------- | --------- |
| Latest `v0.x` release | Yes |
| Any earlier release | No |

## Reporting a vulnerability

Report privately through GitHub: **[open a draft advisory](https://github.com/colonel-byte/cargoship/security/advisories/new)**. Do not open a public issue, and do not include a working exploit against a third party's cluster.

You should expect an acknowledgement within 3 business days and an assessment -- whether we consider it a vulnerability, and the severity we assign -- within 10 business days. Confirmed issues are fixed in the next release, and we aim to publish the advisory within 90 days of the report, sooner when a fix is already out. You will be credited in the advisory unless you ask not to be.

Include the output of `cargoship version`, the host OS of the management node, the Kubernetes engine involved (K3s or RKE2), the commands that reproduce the behaviour, and what an attacker gains.

## Scope

In scope: the `cargoship` CLI and the packages under `src/`; package signing and the verification path (`--verify`, `--key`); credential handling in `clustercfg`, including the vault splice and age encryption; SSH orchestration against target hosts; and the release workflow and the artifacts it publishes.

Out of scope: vulnerabilities in K3s, RKE2, containerd, or any other upstream component Cargoship packages -- report those to the project that owns them; anything that requires an attacker to already have root on the management node, which by design holds the cluster's credentials; resource exhaustion from deliberately malformed or oversized packages supplied by the operator themselves; and misconfiguration of a user's own cluster.

## Verifying a release

Release archives, `checksums.txt`, and container images are signed with cosign using the key whose public half is [`cosign.pub`](../cosign.pub) in this repository, and archives ship with an SBOM.

```console
cosign verify-blob --key cosign.pub --bundle checksums.txt.sigstore.json checksums.txt
cosign verify --key cosign.pub ghcr.io/colonel-byte/cargoship:<tag>
```

See [`docs/dev/goreleaser.md`](../docs/dev/goreleaser.md) for the full set of published artifacts.

## Security in CI

Pull requests and `main` are scanned by CodeQL, dependencies are updated by Dependabot, and supply-chain practices are scored weekly by OpenSSF Scorecard. Results appear in the repository's Security tab.
