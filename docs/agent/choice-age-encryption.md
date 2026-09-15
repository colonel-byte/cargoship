# Why age sits beside Ansible Vault rather than replacing it

Cargoship encrypts four registry credential fields per registry -- `auth.user`, `auth.pass`, `auth.token`, `tls.ca` -- and decrypts them at apply time, when the engine's registry configuration is written. That was Ansible Vault only, built on a single shared passphrase (`src/internal/clustercfg/vault.go`). It now also supports [age](https://github.com/FiloSottile/age), and the two coexist permanently.

The operator-facing guide is [age-encryption](../guides/age-encryption.md). This document records the decisions behind it, and in particular the one thing a future change is most likely to get wrong: **the probe in `EncryptConfig` cannot be re-added for age.**

## Coexistence rather than migration

Both formats carry a self-identifying header: `$ANSIBLE_VAULT` and, because age values are armored to live in a YAML scalar, `-----BEGIN AGE ENCRYPTED FILE-----`. Decryption therefore dispatches on the value itself, and one document can hold both formats with no ambiguity.

That makes coexistence nearly free, so it was chosen over a migration:

- No schema field naming the format, so no field that can disagree with the value beside it.
- No mode flag, so no command whose meaning depends on an invocation the reader of the file cannot see.
- No migration deadline, so a legacy vaulted credential can stay as it is indefinitely.

`cluster.IsEncrypted` (`src/api/zarf.dev/v1alpha1/cluster/spec.go`) is the union of the two checks and is what non-crypto code asks. `AgeHeader` is spelled out there rather than taken from `filippo.io/age/armor`, so the package defining the public API types stays free of crypto dependencies; `TestAgeHeaderMatchesArmorHeader` pins the constant to `armor.Header` so the two cannot drift.

The `vault` command group keeps its name despite now covering both formats. Renaming it would break every operator's scripts to fix a word; the group's `Long` string says what it covers instead.

## Why the probe cannot be re-added

`EncryptConfig` probe-decrypts any value that is already ciphertext. The probe answers exactly one question -- *is this value under the same key I am about to encrypt with?* -- and it exists because `DecryptRegistryAuth` applies one vault password to every field in the document. A file encrypted under two vault passwords is a file no apply can read, so the probe turns that state into an error at write time rather than a failure on the cluster later.

For age recipients that probe is **impossible**, and it is also **not protecting anything**.

It is impossible because age ciphertext does not identify its recipients. The X25519 stanza is `-> X25519 <ephemeral share>`: two fields, no key hash, no fingerprint. That is deliberate -- ciphertext anonymity is a design goal of the format -- and it means only the *count* and *type* of stanzas can be read without a private key. A recipient is a public key, which cannot decrypt. There is nothing to compare and no way to compare it.

It is unnecessary because the invariant it defends is a property of the vault code path, not of encryption in general. `age.Decrypt` is variadic over identities, an identity file holds many, and every configured identity is tried. A document whose values are encrypted to different recipient sets is readable by anyone holding a matching key for each, so "several keys in one document" is a supported state for age rather than the broken one it is for vault.

So `EncryptConfig` probes only when the existing value is Ansible Vault **and** the format being written is Ansible Vault. Every other combination skips the value on the strength of `IsEncrypted` alone, which is all idempotency needs. `TestEncryptConfigSkipsVaultValuesWhenWritingAge` and `TestEncryptConfigAgeIsIdempotent` hold that shape in place.

A future change that tries to restore a general probe will find that it cannot be written, and a change that tries to approximate one -- trial-decrypting with a configured identity, say -- would be answering a different question (*can I read this?*) than the one the probe asks (*is this under the key I am writing with?*), while making `encrypt-file` require a private key it otherwise has no use for.

### What that leaves, and how it is handled

The one real cost is rotation-by-mistake: running `encrypt-file` with a new recipient set against an already-encrypted file does nothing, and the operator may believe it did something. This is addressed by *reporting the skip* rather than by detecting it cryptographically -- the command knows perfectly well that it skipped an encrypted value; what it cannot know is whether that was the right outcome, so it says so and lets the operator decide.

`rewriteConfig`'s `wanted` callback is the only thing that knows why a value was left alone, so it carries the reason out as a `Skip{Path, Reason}`. An empty reason means the skip is not worth reporting, which keeps absent paths, empty values, and the vault-under-the-same-password case -- where the probe *did* answer the question -- silent without every caller filtering them itself. `skipReason` names `rekey` in all three cases it covers, because `rekey` is what does the thing the operator was probably attempting.

The placement of the warnings matters as much as their text. `finishVaultFile` emits them **before** both its `--dry-run` branch and its `len(changed) == 0` early return, since a run that changed nothing is exactly the run the reporting exists for; emitting them after either would leave the loudest failure as the quietest output. They go to stderr through the logger, so a dry run piped to a file still yields a clean document.

Changing which keys a document is encrypted to is `rekey`'s job, and always was. `rekey` reads with one key and writes with another, so cross-format migration falls straight out of the shape it already had: `rekey --vault-password-file old.txt --age-recipient age1...` is the whole vault-to-age path, with no new command and no window in which plaintext touches the disk.

## Why encryption picks a format but decryption never does

Decryption reads the header. Encryption has to choose, and the rules are in `Keyring.EncryptFormat`:

1. An age recipient beats a vault password that came from the **environment**. `CARGOSHIP_VAULT_PASSWORD` and `ANSIBLE_VAULT_PASSWORD` are typically exported once in a shell profile on a machine already using vault. Treating that as a conflict would make `--age-recipient` unusable in exactly the place age is most wanted.
2. An age recipient together with an explicit `--vault-password-file` is an error rather than a third precedence rule. Both flags name the key to write with, a value is written in one format, and choosing on the operator's behalf means writing a secret under a key they did not pick. That is the one case worth stopping for instead of guessing well.
3. Otherwise vault, unchanged.

The keyring therefore has to remember *where* each piece of key material came from, which is what the `vaultExplicit` and `ageExplicit` fields are for. A recipient from the config file's `age` section counts as explicit: committing a recipient set once is the intended way for a team to work, and it is as deliberate as typing the flag.

`RekeyTarget` refuses the same collision for the same reason -- `--new-vault-password-file` and `--age-recipient` each name the key to rekey *onto*.

## Why nothing is discovered

Key material comes from flags, the config file's `age` section, `CARGOSHIP_AGE_IDENTITY_FILE`, and `CARGOSHIP_AGE_RECIPIENTS`. Nothing else. Cargoship does not read `~/.config/sops/age/keys.txt`, does not consult an agent, and never prompts.

This matches `ResolveVaultPassword`, which has never guessed or prompted, and the reason is the same: an apply that succeeds on one operator's machine because of a file the configuration never mentions is an apply nobody can reason about, and the failure mode shows up on someone else's machine, mid-cluster-change.

The environment variables differ in shape for a concrete reason. `CARGOSHIP_AGE_RECIPIENTS` is a whitespace-separated list because a public key contains no whitespace, so the split is unambiguous. `CARGOSHIP_AGE_IDENTITY_FILE` holds a single path, because a list of paths needs a separator and every separator worth choosing is legal in a file name.

A recipients file that parses to no keys is an error rather than nothing to do. Carrying on would leave a keyring with a vault password and no recipients, and `EncryptFormat` would then quietly choose Ansible Vault -- writing the secret under a key the operator did not ask for, which is precisely the outcome rule 2 above exists to prevent.

## Scope deliberately left out

- **SSH keys** via `filippo.io/age/agessh`. A later change; it widens who can be a recipient without touching any of the reasoning above.
- **age passphrases (scrypt).** It would duplicate what Ansible Vault already provides here -- a shared secret -- and age enforces scrypt-as-sole-recipient with a random label, which complicates the recipient path for no gain.
- **Plugins.** These are a property of the `age` binary. Cargoship links the library so that encrypting and decrypting stays a pure in-process operation in a single static binary, the same reason recorded in [choice-vault-library](choice-vault-library.md).

`ParseRecipients` may return a `*HybridRecipient` (post-quantum X-Wing). Passing it through costs nothing, so it works without any code of ours.

## Dependency

`filippo.io/age` v1.3.2, BSD-3-Clause, which brings `filippo.io/hpke` v0.4.0 with it. Everything else it needs -- `chacha20poly1305`, `curve25519`, `hkdf`, `scrypt` -- was already vendored under `vendor/golang.org/x/crypto/`.

`ATTRIBUTION.md` covers *derived* code (k0sctl, zarf, bootloose) rather than dependencies, so it needs no entry; the vendored `LICENSE` is sufficient.

## Armoring, and the one thing it costs

age values are armored because they live in YAML scalars and the binary format does not. Two consequences are worth knowing:

- The age writer must be closed before the armor writer, and both must be closed. The first encrypts and flushes the final chunk; the second writes the footer that terminates the block. Closing only the outer one produces a truncated value that looks fine until something reads it.
- An armored value starts with `-----`, so passing one as a positional argument to `cargoship vault decrypt` is parsed as a flag. Nothing can be done about that inside the command, since the flag parser runs first, so stdin is the documented path and `--` after all flags is the alternative. `02_vault_encrypt_test.go` and `08_age_test.go` both exercise the working forms.
