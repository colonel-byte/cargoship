# What the OpenTofu provider's schema may hold, and what it may not

A tofu state file is a record of everything a resource knows, written in plaintext. It is committed, pushed to remote backends, and readable by everyone with access to that backend. So any attribute the `cargoship_cluster` resource accepts or computes is, in practice, published to whoever can read the state - and a published schema is hard to narrow afterwards, because narrowing it breaks every configuration already using it.

That makes this a decision to take before any attribute name exists, which is why it is recorded here with no provider code written yet (see [choice-tofu-provider-layout](choice-tofu-provider-layout.md) for where that code will live). Four rules.

## 1. Key paths, never key material

The resource takes the **path** to an SSH private key. It never takes the key itself, and there is no attribute that would accept one. A path in state names a file; key material in state is the key, distributed.

Nothing has to be invented for this, because it is already the shape of the configuration: a host's connection settings come from rig's `ssh.Config`, which has `KeyPath *string` and no serializable password field at all, and `ZarfHost` embeds that config inline. A passphrase for an encrypted identity is resolved by callback at connection time (`internal/clustercfg/keyring.go`'s `sshPassphrase`), not read from a field. The inventory format the provider renders therefore has nowhere to put a key even if someone wanted to, and the provider schema should not add one.

The cost is real and accepted: a key path is only meaningful on the machine running tofu, so a provider configuration is not portable between operators without each of them staging the key at the same path or overriding the path themselves. That is the ordinary situation for an airgapped management node, and it is better than the alternative.

## 2. The vault password comes from the environment

There is no `vault_password` attribute, and no `age_identity` attribute carrying an identity. The provider resolves them the way the CLI already does, through `internal/clustercfg.ResolveVaultPassword` and the `Keyring`:

| What            | Where it comes from                                                                       |
| --------------- | ----------------------------------------------------------------------------------------- |
| Vault password  | `CARGOSHIP_VAULT_PASSWORD`, then `ANSIBLE_VAULT_PASSWORD`, or a password file by path     |
| Age identity    | `CARGOSHIP_AGE_IDENTITY_FILE`, or an identity file by path                                |
| Age recipients  | `CARGOSHIP_AGE_RECIPIENTS`, or a recipients file by path                                  |

Reusing that resolution rather than adding attributes has a second benefit beyond keeping state clean: the precedence is defined in one place, so a password that works for `cargoship install apply` works for `tofu apply` without being configured twice.

Encrypted values in the cluster configuration are unaffected. A vault or age ciphertext in a registry credential is already safe to hold in state in the sense that matters - it is ciphertext - and the key that opens it is what this rule keeps out.

## 3. Everything that can carry a secret is marked sensitive, and the marking is data

Some values can still be a credential, because the configuration format permits plaintext: `ZarfClusterRegistryAuth`'s `user`, `pass` and `token` each accept plaintext as well as the two ciphertext formats, and `signing_key_password` in the cargoship configuration file is a password outright. Every provider attribute that maps onto one of those is declared `Sensitive: true`.

Which values those are was documented only in prose and in example ciphertext, which is not something a second module can read. So the schema generator now carries the marking as data: `jsonschema_extras:"x-sensitive=true"` on the field puts `"x-sensitive": true` on the property in `schema/*.json`, and the docs renderer leads that property's description with **Sensitive.** in `docs/schema/`. `TestSensitivePropertiesAreMarked` pins the set, because a field losing the tag is a secret landing in state in plaintext and nothing else in the test suite would notice.

The provider reads the marking from the generated schema rather than keeping its own list. A hand-maintained list in another module is a list that goes stale, and the failure is silent.

Marking an attribute sensitive keeps it out of plan output and logs. It does **not** keep it out of state - nothing does - which is what the next rule is about.

## 4. The kubeconfig is opt-in, not a default output

`KubeConfig.Bytes()` (`pkg/phase/80_kubeconfig.go`) returns a kubeconfig whose embedded client certificate is cluster-admin. It exists because [#305](https://github.com/colonel-byte/cargoship/issues/305) separated building the config from writing it, so a caller can have the value without touching the operator's `~/.kube/config`.

Exposing it as a computed attribute means cluster-admin credentials in every state file, marked sensitive or not. So the provider does not expose it unless asked: an explicit input turns the output on, which maps onto `Write: false` plus a `Bytes()` read, and the attribute is sensitive when it is populated.

The default is the useful one in practice. An operator who wants a kubeconfig on the management node already has `cargoship install kube-config`, which writes the file and merges it, and the phase's `Write`/`Path` fields are exactly that path. Pulling admin credentials through tofu state to get a file on the same machine is the expensive way to do it.

## What this does not decide

Whether a removed host can be reconciled from the provider's state. It cannot, for reasons that are about staleness rather than secrets, and [choice-removed-hosts](choice-removed-hosts.md) records them - but it is worth noting the two decisions reinforce each other. A removal path driven from state would need the removed host's SSH credentials to have survived in that state, so building one would settle this document's first rule the wrong way, by making it impossible to honour.
