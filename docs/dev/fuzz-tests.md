# Running the Fuzz Tests

The `src/fuzz` package holds Go's native fuzz targets. It is not an e2e suite and does not sit with them: it does not drive the built binary and starts no containers, and the targets call the cargoship packages in process, so they run at tens of thousands of executions per second and reach the encoding decisions -- scalar style, quoting, indentation, byte offsets -- where a wrong answer produces a file that still parses and fails much later. Nothing here needs `build/`, a cluster, or the network.

It also has to stay out of `src/test/`. OpenSSF Scorecard's file walker discards every path beginning `src/test/` before any check reads it -- `isTestdataFile` in [`checks/fileparser/listing.go`](https://github.com/ossf/scorecard/blob/main/checks/fileparser/listing.go), which carries the Maven `src/test/java` convention -- so for as long as these targets lived at `src/test/e2e/fuzz` the Fuzzing check scored 0 and reported the project as not fuzzed. Moving the package back under `src/test/` would silently take it to 0 again.

## Layout

```
src/fuzz/main_test.go                    package documentation
src/fuzz/keyring_test.go                 the shared keyrings, one per format, built once in TestMain
src/fuzz/vault_value_fuzz_test.go        value-level targets: EncryptValue/DecryptValue/FormatOf
src/fuzz/vault_path_fuzz_test.go         path-level targets: EncryptAtPath/DecryptAtPath/RekeyAtPath against a fixed document
src/fuzz/vault_document_fuzz_test.go     document-level targets: the splice against varying document shapes, arbitrary documents, and arbitrary paths
src/fuzz/keymaterial_fuzz_test.go        key-material targets: AgeRecipientsIn, ResolveKeyring, RekeyTarget
src/fuzz/ansible_inventory_fuzz_test.go  Ansible inventory targets: the projected request, one host variable, the role mapping
src/fuzz/testdata/fuzz/<Target>/         committed crashers, one directory per target
```

## The Ansible inventory targets

`ansible_inventory_fuzz_test.go` fuzzes `src/internal/ansibleinv`, which is the translation an Ansible module runs on input it did not write. The vault targets guard what a wrong answer does to a file; these guard what a wrong answer does to a cluster, so their properties are about topology rather than encoding:

*   `FuzzAnsibleRequest` takes the JSON an action plugin projects, decodes it, and translates it. The first host in the result has to be a controller, because `ConfigureEngine` makes the first controller the leader; every host has to reach the document with an address, a user and a non-zero port; and translating the same input twice has to produce the same bytes, since the walk goes over Go maps.
*   `FuzzAnsibleHostVar` varies one host variable against an inventory that is otherwise fixed. A variable under the `cargoship_` prefix that cargoship does not read is refused by name. A variable outside the prefix that cargoship does not read leaves the generated document byte-for-byte unchanged.
*   `FuzzAnsibleRoleGroups` varies the role mapping and the group names it points at. No host reaches the document twice, and controllers come before workers.

They need no key material, but they are not all fast: `FuzzAnsibleRequest` translates a request and manages tens of thousands of executions per second, while `FuzzAnsibleHostVar` translates the inventory twice on every execution, with and without the variable, and manages under a thousand. Measured numbers are in the table under [Running](#running); a minute each is a real sweep for the first two and a thin one for `FuzzAnsibleHostVar`.

## Both formats, every time

cargoship writes two ciphertext formats, Ansible Vault and age, and one keyring can carry both. Every target that encrypts therefore runs its body once per format rather than picking one: `encryptKeyrings()` returns the two encrypting keyrings and `rekeyPairs()` returns the four rekey directions (vault to vault, vault to age, age to age, age to vault). Both are in `keyring_test.go`, and both are built once in `TestMain` so that key generation is not paid per execution.

`TestMain` also unsets `CARGOSHIP_AGE_IDENTITY_FILE`, `CARGOSHIP_AGE_RECIPIENTS`, `CARGOSHIP_VAULT_PASSWORD` and `ANSIBLE_VAULT_PASSWORD` before it builds anything. A developer who has those set in their shell would otherwise be fuzzing against different key material than CI, which is the kind of difference that makes a reported crasher unreproducible.

## Running

A plain `go test` runs the **seed corpus only** -- the `f.Add` values in each target, plus anything committed under `testdata/fuzz/<Target>/`. That is about half a second and is what `mage test:fuzz` and `go test ./...` do. No CI job runs this package today -- `e2e.yaml` runs the non-cluster group and `e2e-cluster.yaml` the cluster group, and neither glob reaches it:

```console
$ mage test:fuzz
$ go test -mod=vendor -count=1 ./src/fuzz/     # the same thing, spelled out
```

Actual fuzzing needs `-fuzz`, which takes **one target at a time** and runs until it finds a failure or the clock runs out:

```console
$ go test -mod=vendor -count=1 -run=XXX -fuzz=FuzzDecryptAtPathRoundTrip -fuzztime=60s ./src/fuzz/
```

*   `-run=XXX` matches no unit test, so the run spends its whole budget on the target rather than on the rest of the package.
*   `-fuzztime` defaults to forever. Give it a value unless you mean to sit and watch.
*   `-fuzzminimizetime` bounds the shrinking pass that runs after a failure is found. The default is fine; raise it when the reported input is bigger than it needs to be.
*   `-count=1` disables the test cache, which otherwise makes a re-run of the seed corpus a no-op.

Because only one target runs per invocation, a sweep is a loop:

```console
$ for t in $(grep -ho '^func \(Fuzz[A-Za-z]*\)' src/fuzz/*_test.go | cut -d' ' -f2); do
      go test -mod=vendor -count=1 -run=XXX -fuzz=$t -fuzztime=5m ./src/fuzz/ || break
  done
```

Expect very different throughput between targets. The ones that only push strings through a parser run at tens of thousands of executions per second; the ones that encrypt a value run the Ansible Vault key derivation on every execution and manage fewer than two hundred; the inventory targets fall between the two, since generating a document is cheaper than deriving a key and dearer than parsing a path. The spread across a 30 second budget is roughly three orders of magnitude:

```
FuzzRekeyTargetPicksOneKey                1,806,844      no encryption, key selection only
FuzzPathIsDecryptable                      2,033,626      path parsing only
FuzzDecryptValueRejectsGarbage             2,531,710      almost every input is rejected before any key derivation
FuzzAnsibleRequest                           401,831      decode, translate, translate again, encode both to compare bytes
FuzzAnsibleRoleGroups                         93,138      one translation over a varying role mapping
FuzzAnsibleHostVar                            24,171      two translations per execution, both encoded to YAML on the unchanged path
FuzzSpliceAcrossDocumentShapes                 3,664      one vault encryption per execution
FuzzDecryptAtPathRoundTrip                     2,082      encrypt, splice, decrypt, re-encrypt
FuzzRekeyAtPathPreservesValue                  1,498      four rekey directions per execution
```

Both kinds are useful, but a target that encrypts needs minutes where a parsing target needs seconds. The cost is Ansible Vault's PBKDF2 and is not worth engineering around: it is the same derivation an operator pays, and lowering it would mean fuzzing against key material the product does not use.

## The corpus

There are two corpora, and only one of them is in the repository.

Inputs the fuzzer generates live in the build cache, under `$(go env GOCACHE)/fuzz/`. They are shared across runs on the same machine, are not portable, and are not meant to be committed; `go clean -fuzzcache` discards them, which is worth doing when a target has been rewritten and its cached corpus is exercising a shape that no longer exists.

Failing inputs are the committed corpus. When a target fails, `go test` writes the input to `src/fuzz/testdata/fuzz/<Target>/<hash>` and prints the path. **Commit that file.** From then on it is replayed by a plain `go test`, which is how a crash found by a long fuzz run becomes a permanent regression test that costs milliseconds. The four currently committed are the defects found when these targets were written:

```
FuzzDecryptAtPathRoundTrip/89831cc049267b2c              string("\n")  a credential of one line break, written as an empty block scalar, read back as ""
FuzzPathIsDecryptable/477bd66902831e69                   string("0[0") an unclosed index, which panicked inside go-yaml instead of erroring
FuzzPathIsDecryptable/ebc8a2cafef15425                   string("'$'") a path whose canonical spelling, "$.$", the parser then rejects
FuzzEncryptAtPathArbitraryDocument/d370f7a72740b34b      a flow mapping holding a bare entry, into which the splice wrote a block scalar
FuzzAnsibleRequest/1aea00cb191b2f7c                      a group listing a host with an empty name, which became an address, a hostname and a hostvars key
```

The Ansible one is fixed. A group entry of `""` produced a host whose address, hostname and `hostvars` key were all the empty string, and the schema accepted all three because each asks for a string; nothing downstream could tell that host from one an operator meant to install. `deriveRoles` now refuses an empty host name and an empty group name in the mapping.

The last of those is open. `EncryptAtPath` writes a literal block scalar into a flow mapping such as `{0, pass: 00}`, producing a document that no longer parses, with the credential already encrypted into it and the plaintext gone. `inFlowCollection` in `src/internal/clustercfg/vaultpath.go` is meant to catch exactly this and misses when a bare entry -- a key with an implicit null value -- precedes the target key. Until it is fixed a plain `go test` of this package is red, which is the intended state: the corpus entry is the defect report.

Those same failures are also written up as ordinary table cases next to the code they broke -- see `TestEncryptAtPathReadsBackWhatDecryptWrote` and `TestCanonicalYAMLPath` in `src/internal/clustercfg/vaultpath_test.go`. Keep doing both: the corpus file is what stops the target regressing, and the named case with a comment is what explains the defect to the next reader.

## Writing a target

A target is a function taking `*testing.F`, seeded with `f.Add` and run with `f.Fuzz`. The fuzzed arguments may only be `[]byte`, `string`, the integer and float types, `bool`, and `rune` -- no structs, no slices of anything else. Build the structure you need inside the body from those pieces.

```go
func FuzzEncryptValueRoundTrip(f *testing.F) {
	f.Add("hunter2")
	f.Add("")
	f.Add("\xff\xfe not valid utf-8")

	f.Fuzz(func(t *testing.T, value string) {
		encrypted, err := clustercfg.EncryptValue(value, fuzzKeyring)
		require.NoError(t, err)

		decrypted, err := clustercfg.DecryptValue(encrypted, fuzzKeyring)
		require.NoError(t, err)
		require.Equal(t, value, decrypted, "value did not survive the round trip")
	})
}
```

Four properties are worth stating, and each of them has already caught a real defect in this repo:

*   **Round trip.** `decode(encode(x)) == x`. Caught a credential holding a control character, which `decrypt-path` wrote as a `"\xNN"` escape that `encrypt-path` then refused to measure, and a credential of one line break, which was written as an empty block scalar and read back as the empty string.
*   **Idempotence, and output that is legal input.** `f(f(x)) == f(x)`, and `f`'s output parses. Caught `CanonicalYAMLPath` turning `'$'` into `$.$`, a spelling the parser rejects, which was then handed to the rest of the run.
*   **Two code paths that must agree.** A predicate against the thing it predicts, or a validator against its consumer. `FuzzPathIsDecryptable` states that every path `vault encrypt-path` accepts canonicalises to one the apply-time decryptor visits, because a path that fails that test vaults a credential nothing unwraps.
*   **No panic on untrusted input.** Anything reached by a hand-edited file or a command-line argument. Caught go-yaml reading past the end of a path whose index is never closed -- `registries[0` -- which reached the operator as a stack trace rather than a usage error.

Seed with the shapes the value actually takes plus the ones most likely to be mishandled: empty, whitespace a trim would eat, a PEM block, a control character, invalid UTF-8, and a real fixture such as `src/test/e2e/noncluster/testdata/inventory-vault.yaml`. Seeds are also the whole of what CI runs, so a seed is the cheapest place to pin a shape that matters.

Keep the body fast and self-contained. A target that touches the network or a real file drops from tens of thousands of executions per second to hundreds and finds nothing in the time it is given; use `t.TempDir()` where a file is unavoidable.

Include the input in failure messages. `require.NoError(t, err, "%q canonicalises to %q, which does not parse", path, canonical)` names the defect in the failure line; without it the message says only that something did not parse, and the input has to be dug out of the corpus by hand.

## Gotchas

*   **A target whose oracle is regenerated per run will disagree with the cached corpus.** Vault ciphertext is salted, so a value encrypted now does not equal the same value encrypted in the run that produced the corpus entry. State the property (`cluster.IsVaultEncrypted(blob)`) rather than comparing against a freshly computed ciphertext.
*   **Reading a single YAML path is not the same as decoding the document.** `goyaml.Path.Read` drops the trailing newline of a clipped block scalar where `goyaml.Unmarshal` keeps it, which fails a PEM round trip for a reason that has nothing to do with the code under test. Decode into `cluster.ZarfCluster`, which is also what an apply does.
*   **A failing seed stops the run before the fuzzing starts.** `failure while testing seed corpus entry` means the target is red on input it was given, not on input it found. Fix that first; until it is fixed the target is not fuzzing at all.
*   **A target that varies the document shape has to vary it the way a real file does.** `blockScalar` in `vault_document_fuzz_test.go` takes a chomp indicator because the TLS CA neighbour is a PEM block that keeps its trailing newline; rendering it with `|-` strips that newline and fails the neighbour assertion for a reason that has nothing to do with the splice.
*   **Line endings are deliberately not one of the shape knobs.** A document saved with CRLF is one the splice mishandles today: `DecryptAtPath` refuses a multi-line value in such a document, and `spliceScalar` writes bare line feeds into it when it does splice one. Fuzzing that axis reports the same known defect on every input rather than finding a new one. Put the knob back once the splice carries the document's own line ending.

## Where to look next

Fuzzing pays where a wrong answer still parses, so the targets worth writing next are the places that choose an encoding, canonicalise a string, or hand a path to a parser:

*   `distrocfg.marshalRegistriesYAML` and `quoteRegistryKeys` (`src/types/distrocfg/distro_common.go`) choose a quoting style for keys the engine reparses -- the same shape as the bugs above, over registry names and `rewrite` regex patterns.
*   `split.SplitFile` and `split.ReassembleFile` are a round trip over content bytes and a chunk size, and `ReassembleFile` trusts the `part000` metadata it unmarshals from disk.
*   `cfg.Parse`, `cfg.ParseMultiDoc` and `utils.ReadByteStrict` take document bytes straight into a third-party parser, which is exactly where the panics found so far have come from.
*   `isCleanPathRegex` (`src/cmd/common.go`) is a path sanitiser, so the property is that an accepted path, joined to a base and cleaned, stays under that base.
*   `dns.ParseServiceURL` against `dns.IsServiceURL` is a predicate and its implementation, the differential shape.
