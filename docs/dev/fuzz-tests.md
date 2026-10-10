# Running the Fuzz Tests

The `fuzz` package holds Go's native fuzz targets. It is not an e2e suite and does not sit with them: it does not drive the built binary and starts no containers, and the targets call the cargoship packages in process, so they run at tens of thousands of executions per second and reach the encoding decisions -- scalar style, quoting, indentation, byte offsets -- where a wrong answer produces a file that still parses and fails much later. Nothing here needs `build/`, a cluster, or the network.

It also has to stay out of `src/test/`. OpenSSF Scorecard's file walker discards every path beginning `src/test/` before any check reads it -- `isTestdataFile` in [`checks/fileparser/listing.go`](https://github.com/ossf/scorecard/blob/main/checks/fileparser/listing.go), which carries the Maven `src/test/java` convention -- so for as long as these targets lived at `src/test/e2e/fuzz` the Fuzzing check scored 0 and reported the project as not fuzzed. Moving the package back under `src/test/` would silently take it to 0 again. The e2e suites themselves moved out of `src/test/` to `test/` for unrelated reasons, which loses that same exclusion for their tree -- worth knowing if a future Scorecard run starts flagging something in `test/` that it did not before.

## Layout

One file per concern: 49 targets across 24 target files. The shared scaffolding:

```
fuzz/main_test.go                    package documentation and TestMain
fuzz/keyring_test.go                 the shared keyrings, one per format, built once in TestMain
fuzz/testdata/fuzz/<Target>/         committed crashers, one directory per target
fuzz/AGENTS.md                       the naming rule: no target name may be a prefix of another
```

The targets, grouped by what they guard:

```
vault_value_fuzz_test.go             EncryptValue/DecryptValue/FormatOf round trips and salting
vault_path_fuzz_test.go              EncryptAtPath/DecryptAtPath/RekeyAtPath and path canonicalisation
vault_document_fuzz_test.go          the splice against varying document shapes, arbitrary documents, arbitrary paths
vault_config_fuzz_test.go            EncryptConfig/DecryptConfig over a whole document
vault_meta_fuzz_test.go              the recipients recorded in a document's vault metadata
keymaterial_fuzz_test.go             AgeRecipientsIn, ResolveKeyring, RekeyTarget
parse_recipients_fuzz_test.go        age recipient lines and recipient lists
cfg_fuzz_test.go                     cfg.Parse and cfg.ParseMultiDoc over arbitrary document bytes
clustercfg_parse_fuzz_test.go        the cluster inventory parser
schema_decode_fuzz_test.go           schema decoding
url_and_config_fuzz_test.go          ExtractBasePathFromURL and ReadByteStrict over a distro config
dns_fuzz_test.go                     ParseServiceURL and IsLocalhost
layout_path_fuzz_test.go             the path sanitisers: IsContainedPath and isCleanPath
parse_manifest_fuzz_test.go          OCI manifest parsing
parse_checksum_fuzz_test.go          checksum lines and Retry-After headers
parse_registry_overrides_fuzz_test.go  the --registry-override key=value list
file_overrides_fuzz_test.go          file override parsing and resolution
identify_source_fuzz_test.go         package source identification
helmvalues_parse_fuzz_test.go        values file shape and schema external-ref rejection
helmvalues_path_fuzz_test.go         value path round trips, mapping application, merge immutability
helmvalues_template_fuzz_test.go     template rendering, including that it denies host state
nft_split_families_fuzz_test.go      nftables family splitting
ansible_inventory_fuzz_test.go       the projected request, one host variable, the role mapping
ansiblemod_fuzz_test.go              module argument reading and module naming
```

## The Ansible inventory targets

`ansible_inventory_fuzz_test.go` fuzzes `internal/ansibleinv`, which is the translation an Ansible module runs on input it did not write. The vault targets guard what a wrong answer does to a file; these guard what a wrong answer does to a cluster, so their properties are about topology rather than encoding:

*   `FuzzAnsibleRequest` takes the JSON an action plugin projects, decodes it, and translates it. The first host in the result has to be a controller, because `ConfigureEngine` makes the first controller the leader; every host has to reach the document with an address, a user and a non-zero port; and translating the same input twice has to produce the same bytes, since the walk goes over Go maps.
*   `FuzzAnsibleHostVar` varies one host variable against an inventory that is otherwise fixed. A variable under the `cargoship_` prefix that cargoship does not read is refused by name. A variable outside the prefix that cargoship does not read leaves the generated document byte-for-byte unchanged.
*   `FuzzAnsibleRoleGroups` varies the role mapping and the group names it points at. No host reaches the document twice, and controllers come before workers.

They need no key material, but they are not all fast: `FuzzAnsibleRequest` translates a request and manages tens of thousands of executions per second, while `FuzzAnsibleHostVar` translates the inventory twice on every execution, with and without the variable, and manages under a thousand. Measured numbers are in the table under [Running](#running); a minute each is a real sweep for the first two and a thin one for `FuzzAnsibleHostVar`.

## Both formats, every time

cargoship writes two ciphertext formats, Ansible Vault and age, and one keyring can carry both. Every target that encrypts therefore runs its body once per format rather than picking one: `encryptKeyrings()` returns the two encrypting keyrings and `rekeyPairs()` returns the four rekey directions (vault to vault, vault to age, age to age, age to vault). Both are in `keyring_test.go`, and both are built once in `TestMain` so that key generation is not paid per execution.

`TestMain` also unsets `CARGOSHIP_AGE_IDENTITY_FILE`, `CARGOSHIP_AGE_RECIPIENTS`, `CARGOSHIP_VAULT_PASSWORD` and `ANSIBLE_VAULT_PASSWORD` before it builds anything. A developer who has those set in their shell would otherwise be fuzzing against different key material than CI, which is the kind of difference that makes a reported crasher unreproducible.

## Running

A plain `go test` runs the **seed corpus only** -- the `f.Add` values in each target, plus anything committed under `testdata/fuzz/<Target>/`. That is about half a second and is what `mage test:fuzz` and `go test ./...` do. `.github/workflows/unit-tests.yaml` runs it on every pull request, as a second step beside `test:unit`, because it needs nothing that step did not already need:

```console
$ mage test:fuzz
$ go test -mod=vendor -count=1 ./fuzz/     # the same thing, spelled out
```

Actual fuzzing needs `-fuzz`, which takes **one target at a time** and runs until it finds a failure or the clock runs out:

```console
$ go test -mod=vendor -count=1 -run=XXX -fuzz=FuzzDecryptAtPathRoundTrip -fuzztime=60s ./fuzz/
```

*   `-run=XXX` matches no unit test, so the run spends its whole budget on the target rather than on the rest of the package.
*   `-fuzztime` defaults to forever. Give it a value unless you mean to sit and watch.
*   `-fuzzminimizetime` bounds the shrinking pass that runs after a failure is found. The default is fine; raise it when the reported input is bigger than it needs to be.
*   `-count=1` disables the test cache, which otherwise makes a re-run of the seed corpus a no-op.

Because only one target runs per invocation, a sweep is a loop:

```console
$ for t in $(grep -ho '^func \(Fuzz[A-Za-z]*\)' fuzz/*_test.go | cut -d' ' -f2); do
      go test -mod=vendor -count=1 -run=XXX -fuzz=$t -fuzztime=5m ./fuzz/ || break
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

Failing inputs are the committed corpus. When a target fails, `go test` writes the input to `fuzz/testdata/fuzz/<Target>/<hash>` and prints the path. **Commit that file.** From then on it is replayed by a plain `go test`, which is how a crash found by a long fuzz run becomes a permanent regression test that costs milliseconds. There are 24 committed crashers across 9 targets, the bulk of them under `FuzzEncryptAtPathArbitraryDocument` (12) and `FuzzDecryptAtPathRoundTrip` (3).

Every one of them is fixed -- `go test ./fuzz/` is green. A red run here means a new defect, not a known one. The five that came out of writing the original targets are worth reading as a sample of the shape of defect this package finds:

```
FuzzDecryptAtPathRoundTrip/89831cc049267b2c              string("\n")  a credential of one line break, written as an empty block scalar, read back as ""
FuzzPathIsDecryptable/477bd66902831e69                   string("0[0") an unclosed index, which panicked inside go-yaml instead of erroring
FuzzPathIsDecryptable/ebc8a2cafef15425                   string("'$'") a path whose canonical spelling, "$.$", the parser then rejects
FuzzEncryptAtPathArbitraryDocument/d370f7a72740b34b      a flow mapping holding a bare entry, into which the splice wrote a block scalar
FuzzAnsibleRequest/1aea00cb191b2f7c                      a group listing a host with an empty name, which became an address, a hostname and a hostvars key
```

Two are worth the detail. The Ansible one: a group entry of `""` produced a host whose address, hostname and `hostvars` key were all the empty string, and the schema accepted all three because each asks for a string; nothing downstream could tell that host from one an operator meant to install. `deriveRoles` now refuses an empty host name and an empty group name in the mapping.

The flow-mapping one: `EncryptAtPath` wrote a literal block scalar into a flow mapping such as `{0, pass: 00}`, producing a document that no longer parses, with the credential already encrypted into it and the plaintext gone. `inFlowCollection` in `internal/clustercfg/vaultpath.go` is what catches this, and it missed when a bare entry -- a key with an implicit null value -- preceded the target key. That is fixed; the corpus entry is what keeps it fixed.

Those same failures are also written up as ordinary table cases next to the code they broke -- see `TestEncryptAtPathReadsBackWhatDecryptWrote` and `TestCanonicalYAMLPath` in `internal/clustercfg/vaultpath_test.go`. Keep doing both: the corpus file is what stops the target regressing, and the named case with a comment is what explains the defect to the next reader.

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

Seed with the shapes the value actually takes plus the ones most likely to be mishandled: empty, whitespace a trim would eat, a PEM block, a control character, invalid UTF-8, and a real fixture such as `test/e2e/noncluster/testdata/inventory-vault.yaml`. Seeds are also the whole of what CI runs, so a seed is the cheapest place to pin a shape that matters.

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

*   `distrocfg.marshalRegistriesYAML` and `quoteRegistryKeys` (`types/distrocfg/distro_common.go`) choose a quoting style for keys the engine reparses -- the same shape as the bugs above, over registry names and `rewrite` regex patterns.
*   `split.SplitFile` and `split.ReassembleFile` are a round trip over content bytes and a chunk size, and `ReassembleFile` trusts the `part000` metadata it unmarshals from disk.
