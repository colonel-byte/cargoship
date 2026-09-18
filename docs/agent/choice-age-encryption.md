# Why age sits beside Ansible Vault rather than replacing it

Cargoship encrypts four registry credential fields per registry -- `auth.user`, `auth.pass`, `auth.token`, `tls.ca` -- and decrypts them at apply time, when the engine's registry configuration is written. That was Ansible Vault only, built on a single shared passphrase (`src/internal/clustercfg/vault.go`). It now also supports [age](https://github.com/FiloSottile/age), and the two coexist permanently.

The operator-facing guide is [age-encryption](../guides/age-encryption.md). This document records the decisions behind it, and in particular the two things a future change is most likely to get wrong: **the probe in `EncryptConfig` cannot be re-added for age**, and **the recipient record in `metadata` must never be read back as key material.**

## Coexistence rather than migration

Both formats carry a self-identifying header: `$ANSIBLE_VAULT` and, because age values are armored to live in a YAML scalar, `-----BEGIN AGE ENCRYPTED FILE-----`. Decryption therefore dispatches on the value itself, and one document can hold both formats with no ambiguity.

That makes coexistence nearly free, so it was chosen over a migration:

- No schema field naming the format, so no field that can disagree with the value beside it. The recipient record added later is the one exception, and it is kept inert for exactly this reason -- see [The recipient record, and why it is inert](#the-recipient-record-and-why-it-is-inert).
- No mode flag, so no command whose meaning depends on an invocation the reader of the file cannot see.
- No migration deadline, so a legacy vaulted credential can stay as it is indefinitely.

`cluster.IsEncrypted` (`src/api/zarf.dev/v1alpha1/cluster/spec.go`) is the union of the two checks and is what non-crypto code asks. `AgeHeader` is spelled out there rather than taken from `filippo.io/age/armor`, so the package defining the public API types stays free of crypto dependencies; `TestAgeHeaderMatchesArmorHeader` pins the constant to `armor.Header` so the two cannot drift.

The `vault` command group keeps its name despite now covering both formats. Renaming it would break every operator's scripts to fix a word; the group's `Long` string says what it covers instead.

## Why the probe cannot be re-added

`EncryptConfig` probe-decrypts any value that is already ciphertext. The probe answers exactly one question -- *is this value under the same key I am about to encrypt with?* -- and it exists because `DecryptRegistryAuth` applies one vault password to every field in the document. A file encrypted under two vault passwords is a file no apply can read, so the probe turns that state into an error at write time rather than a failure on the cluster later.

For age recipients that probe is **impossible**, and it is also **not protecting anything**.

It is impossible for two reasons, and the second is the durable one.

An X25519 stanza is `-> X25519 <ephemeral share>`: two fields, no key hash, no fingerprint. Ciphertext anonymity is a design goal of the format. This does **not** generalise to every recipient type -- an `agessh` stanza carries a 32-bit hash of the public key, which is exactly the identifier the probe would want -- so it is not on its own a reason the probe cannot exist.

The reason it cannot is that nothing here can read a stanza at all. `age.ExtractHeader` exists, but the `Stanza` fields it would yield live behind `filippo.io/age/internal/format`, and an internal package is not importable. Cargoship would have to parse the header text itself, duplicating a format it does not own, to recover an identifier that only some recipient types carry. A recipient is a public key, which cannot decrypt; there is nothing to compare against, and no supported way to reach the thing that would be compared.

It is unnecessary because the invariant it defends is a property of the vault code path, not of encryption in general. `age.Decrypt` is variadic over identities, an identity file holds many, and every configured identity is tried. A document whose values are encrypted to different recipient sets is readable by anyone holding a matching key for each, so "several keys in one document" is a supported state for age rather than the broken one it is for vault.

So `EncryptConfig` probes only when the existing value is Ansible Vault **and** the format being written is Ansible Vault. Every other combination skips the value on the strength of `IsEncrypted` alone, which is all idempotency needs. `TestEncryptConfigSkipsVaultValuesWhenWritingAge` and `TestEncryptConfigAgeIsIdempotent` hold that shape in place.

A future change that tries to restore a general probe will find that it cannot be written, and a change that tries to approximate one -- trial-decrypting with a configured identity, say -- would be answering a different question (*can I read this?*) than the one the probe asks (*is this under the key I am writing with?*), while making `encrypt-file` require a private key it otherwise has no use for.

### What that leaves, and how it is handled

The one real cost is rotation-by-mistake: running `encrypt-file` with a new recipient set against an already-encrypted file does nothing, and the operator may believe it did something. This is addressed by *reporting the skip* rather than by detecting it cryptographically -- the command knows perfectly well that it skipped an encrypted value; what it cannot know is whether that was the right outcome, so it says so and lets the operator decide.

`rewriteConfig`'s `wanted` callback is the only thing that knows why a value was left alone, so it carries the reason out as a `Skip{Path, Reason}`. An empty reason means the skip is not worth reporting, which keeps absent paths, empty values, and the vault-under-the-same-password case -- where the probe *did* answer the question -- silent without every caller filtering them itself. `skipReason` names `rekey` in all three cases it covers, because `rekey` is what does the thing the operator was probably attempting.

The placement of the warnings matters as much as their text. `finishVaultFile` emits them **before** both its `--dry-run` branch and its `len(changed) == 0` early return, since a run that changed nothing is exactly the run the reporting exists for; emitting them after either would leave the loudest failure as the quietest output. They go to stderr through the logger, so a dry run piped to a file still yields a clean document.

Changing which keys a document is encrypted to is `rekey`'s job, and always was. `rekey` reads with one key and writes with another, so cross-format migration falls straight out of the shape it already had: `rekey --vault-password-file old.txt --age-recipient age1...` is the whole vault-to-age path, with no new command and no window in which plaintext touches the disk.

## The recipient record, and why it is inert

A document encrypted with age carries `metadata.encryption.age.recipients` and `lastModified`, written by `src/internal/clustercfg/vaultmeta.go`. It is the answer to issue #392, and it is in direct tension with a bullet three sections above: *no schema field naming the format, so no field that can disagree with the value beside it.* This is exactly such a field. It can disagree with the ciphertext beside it, nothing can detect that it has, and for the reasons in the probe section nothing ever will be able to.

It was accepted anyway, under one restriction that the entire justification rests on:

**Cargoship writes this block and never reads it back as key material.** It is read for exactly one purpose -- comparing what the document claims against what the operator just named on the command line -- and the result of that comparison is a warning and nothing else. No command encrypts to a key that came out of the file being edited.

That restriction is not a matter of taste. If `rekey` defaulted its recipients to the recorded list when none were given, a one-line edit in a pull request would silently redirect every credential in the file to the author's key, and the resulting ciphertext would be indistinguishable from a correct rekey -- same armor, same length distribution, same absence of any recipient identifier. The reviewer's only signal would be the YAML line the attacker wrote. So `rekey` with an identity and no recipients still fails with `no age recipients configured`, exactly as it did before the block existed, and that failure is a feature. Any future change that proposes "rekey could just use the recipients already in the file" is proposing this attack; the convenience it buys is real and is not worth it.

What the field does buy, given that restriction, is the thing the probe section says is the one real cost: an operator can read the file and see which keys it was encrypted to, and a reviewer can see a recipient set change as a line of YAML rather than as a wall of re-randomized ciphertext. Those are documentation wins, and documentation is a thing a file is allowed to be wrong about in a way ciphertext is not.

### Placement, and the shape of the block

It sits in `metadata`, beside `name`, rather than in `spec`. `spec` is the desired state an apply acts on, and this is a record of something that already happened to the file; putting it there would invite exactly the reading -- "this is configuration, so something must consume it" -- that the restriction above forbids. The types are in `src/api/zarf.dev/v1alpha1/cluster/spec.go` as plain strings, keeping that package crypto-free the same way `AgeHeader` does.

There is a section per format (`encryption.age`) rather than one flat list, so a document holding both Ansible Vault and age credentials has somewhere to say so later if that ever becomes worth saying. Only age needs a record today: a vaulted value is read with the one password the operator already supplies by name, so there is nothing about it a file could usefully record.

`lastModified` is included despite being churn, because "when was this last re-encrypted" is the question that follows "which keys is it encrypted to" often enough to be worth the line. It is RFC 3339 and always double-quoted, since a bare timestamp is a shape YAML is free to read as something other than a string. The clock is `var now = time.Now` in `vaultmeta.go` rather than four signature changes; a test concern is not worth reshaping three exported functions.

An SSH recipient keeps its `authorized_keys` comment, because that is the part that says whose key it is, and whose key it is was the point. Comparison, though, is on the key itself -- `recipientKey` strips options and comments via `ssh.ParseAuthorizedKey` -- so re-labelling a key or reordering a recipients file does not read as drift. The question the comparison answers is *who can read this file*, and neither of those changes the answer.

### When it is written, and why a no-op writes nothing

`EncryptConfig` writes the record only when it is writing age **and** `len(changed) > 0`. A run that encrypted nothing touches nothing, including the timestamp.

That condition is load-bearing in two directions. It keeps `encrypt-file` byte-for-byte idempotent, which `TestEncryptConfigAgeIsIdempotent` and `TestEncryptConfigLeavesTheRecordAloneWhenNothingChanged` both hold. And it keeps `finishVaultFile`'s `len(changed) == 0` early return honest: without it, a run that reported "nothing to encrypt" would nonetheless have a modified document to write, and the command would either lie or write behind its own report.

`RekeyConfig` rewrites the record when the target writes age and strips it when the target writes vault. `DecryptConfig` strips it unless age ciphertext remains somewhere in the document -- checked with `bytes.Contains(doc, []byte(cluster.AgeHeader))` rather than assumed, since `encrypt-path` can leave age values outside the credentials the whole-file commands walk. A list of age recipients above a file that holds no age ciphertext is worse than no list: it reads as a file that is still protected.

A `metadata` mapping written in flow style -- `metadata: {name: e72}` -- is refused rather than recorded. Flow style has no block collection, so splicing a nested block into one produces a document that no longer parses, with the credentials already encrypted into it. `errNoMetadataBlock` carries that out, `recordRecipients` swallows it, and `reportRecipientRecord` in `src/cmd/misc_vault_encrypt_file.go` turns it into a warning. Declining to record a fact about a file is much the smaller loss, and it is the same reasoning `inFlowCollection` already encodes for the value splice.

Everything in `vaultmeta.go` is a byte-level splice for the reason `spliceScalar` is: go-yaml re-indents multi-line literals and truncates ciphertext nested in sequences, so nothing here re-renders the document.

### Why `skipReason`'s wording stays

The age-to-age skip still says the value is encrypted to age recipients already and that cargoship cannot tell whether they are the ones you named. That remains exactly true -- it is a statement about the ciphertext, and no record changes what the ciphertext says about itself.

The new warning is a different claim about a different thing: what the *document* says it was encrypted to. They are emitted together and they are both worth having. A future change that "fixes" the skip message to mention the record would be merging a cryptographic fact with a piece of documentation, which is the confusion this whole section exists to prevent.

`reportRecipientRecord` lives in `src/cmd` rather than in `clustercfg`, because `clustercfg` does no terminal I/O and the warning is about the document as a whole rather than about one credential path -- so it does not fit `Skip{Path, Reason}`. It is in `encrypt-file` specifically, not in `finishVaultFile`: `decrypt-file` removes the record and `rekey` rewrites it, and in both cases that is the operator getting precisely what they asked for. It is called before `finishVaultFile` for the same reason the skip warnings are emitted where they are -- a run that changed nothing is the run it exists for.

It reads the prior claim from the document as it was read, not as it will be written, since a run that encrypted something has already replaced the record. And it fires only when something was skipped: with no skips, every credential in the file is on the keys just named and the record agrees with itself.

### Compatibility

`ZarfClusterMetadata` carries `additionalProperties: false`, so a document holding this block fails `cargoship validate` on a binary built before it existed. An apply is unaffected -- `clustercfg.Parse` is a non-strict unmarshal, and the generated schema is used only by `cargoship validate` (`src/cmd/misc_validate.go`). Both copies of the schema have to be regenerated together with `mage generate:schema`, since CI does not run the generator.

## Why encryption picks a format but decryption never does

Decryption reads the header. Encryption has to choose, and the rules are in `Keyring.EncryptFormat`:

1. An age recipient beats a vault password that came from the **environment**. `CARGOSHIP_VAULT_PASSWORD` and `ANSIBLE_VAULT_PASSWORD` are typically exported once in a shell profile on a machine already using vault. Treating that as a conflict would make `--age-recipient` unusable in exactly the place age is most wanted.
2. An age recipient together with an explicit `--vault-password-file` is an error rather than a third precedence rule. Both flags name the key to write with, a value is written in one format, and choosing on the operator's behalf means writing a secret under a key they did not pick. That is the one case worth stopping for instead of guessing well.
3. Otherwise vault, unchanged.

The keyring therefore has to remember *where* each piece of key material came from, which is what the `vaultExplicit` and `ageExplicit` fields are for. A recipient from the config file's `age` section counts as explicit: committing a recipient set once is the intended way for a team to work, and it is as deliberate as typing the flag.

`RekeyTarget` refuses the same collision for the same reason -- `--new-vault-password-file` and `--age-recipient` each name the key to rekey *onto*.

## Why nothing is discovered

Key material comes from flags, the config file's `age` section, `CARGOSHIP_AGE_IDENTITY_FILE`, and `CARGOSHIP_AGE_RECIPIENTS`. Nothing else. Cargoship does not read `~/.config/sops/age/keys.txt`, does not consult an agent, and does not reach into `~/.ssh` for a key it was not pointed at.

The one prompt in the whole path is for the passphrase of an SSH key the operator named, and the one derived path is the `.pub` file beside such a key; see below for why neither is an exception to this rule.

This matches `ResolveVaultPassword`, which has never guessed or prompted, and the reason is the same: an apply that succeeds on one operator's machine because of a file the configuration never mentions is an apply nobody can reason about, and the failure mode shows up on someone else's machine, mid-cluster-change.

The environment variables differ in shape for a concrete reason. `CARGOSHIP_AGE_RECIPIENTS` is a whitespace-separated list because a public key contains no whitespace, so the split is unambiguous. `CARGOSHIP_AGE_IDENTITY_FILE` holds a single path, because a list of paths needs a separator and every separator worth choosing is legal in a file name.

A recipients file that parses to no keys is an error rather than nothing to do. Carrying on would leave a keyring with a vault password and no recipients, and `EncryptFormat` would then quietly choose Ansible Vault -- writing the secret under a key the operator did not ask for, which is precisely the outcome rule 2 above exists to prevent.

## Scope deliberately left out

- **age passphrases (scrypt).** It would duplicate what Ansible Vault already provides here -- a shared secret -- and age enforces scrypt-as-sole-recipient with a random label, which complicates the recipient path for no gain.
- **Plugins.** These are a property of the `age` binary. Cargoship links the library so that encrypting and decrypting stays a pure in-process operation in a single static binary, the same reason recorded in [choice-vault-library](choice-vault-library.md).
- **`ssh-agent`.** `agessh` needs the private key itself rather than a signing oracle, so an agent-held key cannot be used even though a passphrase-protected one on disk can.

`ParseRecipients` may return a `*HybridRecipient` (post-quantum X-Wing). Passing it through costs nothing, so it works without any code of ours.

## SSH keys, and the parsing they force

`filippo.io/age/agessh` supplies `age.Recipient` and `age.Identity` implementations for `ssh-ed25519` and `ssh-rsa`, so an SSH key is invisible above `clustercfg`: nothing about the ciphertext, the header dispatch, or the skip reporting changes. What it widens is *who can use age at all*, since operators and CI already hold these keys and already maintain `authorized_keys` files.

They go in the existing `--age-recipient`, `--age-recipients-file` and `--age-identity-file` rather than in parallel `--ssh-*` flags. That is what the `age` CLI does with `-r`/`-R`/`-i`, it keeps nine commands from growing three flags each, and it means an operator does not have to work out which kind of key they hold before choosing a flag.

The cost of that decision is that cargoship has to do its own parsing, in `agessh.go`:

- `age.ParseRecipients` fails the **whole file** on one line it does not recognise, and an SSH line is one. So recipients are scanned line by line here and dispatched on the `age1` prefix, which keeps a mixed file working. A line that parses as neither kind still fails the file, naming the line: skipping it would encrypt to fewer recipients than the operator listed, and the person who discovers that is the one who cannot decrypt.
- Identities cannot be dispatched line by line at all, because an SSH private key is a multi-line PEM block. The file is read whole and routed on a `-----BEGIN ` header.
- `parseRecipient` refuses anything beginning `AGE-SECRET-KEY-` or `-----BEGIN ` before handing it to `agessh`, because `agessh.ParseRecipient` quotes its argument back in its error where age's own parser deliberately does not. An identity file passed as a recipients file is an ordinary mistake, and a private key in a log has to be treated as compromised everywhere it was used.

## Why the SSH passphrase prompt lives in src/cmd

A passphrase-protected SSH key becomes an `agessh.EncryptedSSHIdentity`, which asks for the passphrase only once a stanza matches its public key and caches the decrypted key afterwards. That laziness is worth preserving: an operator holding a key nothing was encrypted to is never asked, and one whose key does match is asked once rather than once per credential. On an apply the ask therefore lands in the `VerifyRegistryAuth` preflight, before any host is touched.

`clustercfg` does no terminal I/O, so `KeyOptions` carries a `PassphraseFunc` and the commands supply it. Two things fall out of that shape:

- **A nil callback is an error, not a prompt.** A caller with nowhere to ask fails by construction rather than by luck, which is what CI wants.
- **The prompt refuses a non-terminal rather than reading stdin.** Reading the passphrase from a pipe would consume input the command wanted for something else and would hang when there is none. Failing with a message naming the key is something a scripted run can act on.

An older PEM key carries no public key beside its encrypted private key, so `ssh.PassphraseMissingError.PublicKey` is nil and the key has to come from the `.pub` file. That is the one path cargoship derives rather than being given, and it is derived from a path the operator named explicitly, read only when that key needs a passphrase -- which is why it does not contradict "nothing is discovered" above. The age CLI does the same.

`AgeRecipientsIn` refuses an SSH private key rather than deriving a recipient from it: `agessh`'s recipient types have no text encoding to print, and `ssh-keygen` already wrote the public key into the file beside the key. Pointing at that is a better answer than any this could compute.

## Generating keys, and the refusals around it

`cargoship vault keygen` exists because everything else here otherwise assumes the `age` distribution is installed, when cargoship already links the library that generates a key pair. It writes byte-for-byte what `age-keygen` writes, deliberately: a file only cargoship could read would strand an operator who later wants `age --decrypt`, and interoperability is the reason for supporting the format at all.

Three refusals are load-bearing, and none of them should be softened into a prompt or an overwrite:

- **`--output` never overwrites.** Replacing an identity file destroys the only copy of a key, and every value in a committed configuration encrypted to it becomes unreadable by anyone, permanently. `O_CREATE|O_EXCL` makes the check part of the create, so there is no window between asking and writing. A `--force` flag here would be a flag whose only use is the unrecoverable case.
- **`--output` is refused with `--public-key`.** It keeps one meaning -- the file holding a private key that was just generated -- and so keeps the no-overwrite rule from having an exception attached to it. A public key needs no such care and redirects perfectly well.
- **An identity that is not X25519 is an error in `AgeRecipientsIn`, not a skipped line.** The question being asked is "who can read values encrypted to this key," and a short answer to that is worse than no answer, because the operator acts on it.

The generated identity never passes through `logger`. Logging is wired to stderr and may be scraped centrally, and a private key that reaches a log aggregator has to be treated as compromised everywhere it was used. Without `--output` the key goes to stdout so that `cargoship vault keygen > key.txt` works; the terminal warning covers the case where there is no redirect and the key lands in scrollback.

## Dependency

`filippo.io/age` v1.3.2, BSD-3-Clause, which brings `filippo.io/hpke` v0.4.0 with it. Everything else it needs -- `chacha20poly1305`, `curve25519`, `hkdf`, `scrypt` -- was already vendored under `vendor/golang.org/x/crypto/`.

`agessh` is a package of that same module, so SSH support added only `filippo.io/edwards25519` v1.2.0, which it uses to convert an Ed25519 key to X25519. `golang.org/x/crypto/ssh` was already vendored, and moved from an indirect dependency to a direct one because `clustercfg` now imports it for `ssh.PublicKey` and `ssh.PassphraseMissingError`.

`ATTRIBUTION.md` covers *derived* code (k0sctl, zarf, bootloose) rather than dependencies, so it needs no entry; the vendored `LICENSE` is sufficient.

## Armoring, and the one thing it costs

age values are armored because they live in YAML scalars and the binary format does not. Two consequences are worth knowing:

- The age writer must be closed before the armor writer, and both must be closed. The first encrypts and flushes the final chunk; the second writes the footer that terminates the block. Closing only the outer one produces a truncated value that looks fine until something reads it.
- An armored value starts with `-----`, so passing one as a positional argument to `cargoship vault decrypt` is parsed as a flag. Nothing can be done about that inside the command, since the flag parser runs first, so stdin is the documented path and `--` after all flags is the alternative. `02_vault_encrypt_test.go` and `08_age_test.go` both exercise the working forms.
