# Running the Fuzz Tests

The `src/test/e2e/fuzz` package holds Go's native fuzz targets. Unlike the other suites under `src/test/e2e`, it does not drive the built binary and starts no containers: the targets call the cargoship packages in process, so they run at tens of thousands of executions per second and reach the encoding decisions -- scalar style, quoting, indentation, byte offsets -- where a wrong answer produces a file that still parses and fails much later. Nothing here needs `build/`, a cluster, or the network.

## Layout

```
src/test/e2e/fuzz/main_test.go                package documentation and the shared vault password
src/test/e2e/fuzz/vault_value_fuzz_test.go    value-level targets: EncryptValue/DecryptValue
src/test/e2e/fuzz/vault_path_fuzz_test.go     document-level targets: EncryptAtPath/DecryptAtPath/RekeyAtPath, and the path predicates
src/test/e2e/fuzz/testdata/fuzz/<Target>/     committed crashers, one directory per target
```

## Running

A plain `go test` runs the **seed corpus only** -- the `f.Add` values in each target, plus anything committed under `testdata/fuzz/<Target>/`. That is about half a second and is what CI and `go test ./...` do:

```console
$ go test -mod=vendor -count=1 ./src/test/e2e/fuzz/
```

Actual fuzzing needs `-fuzz`, which takes **one target at a time** and runs until it finds a failure or the clock runs out:

```console
$ go test -mod=vendor -count=1 -run=XXX -fuzz=FuzzDecryptAtPathRoundTrip -fuzztime=60s ./src/test/e2e/fuzz/
```

*   `-run=XXX` matches no unit test, so the run spends its whole budget on the target rather than on the rest of the package.
*   `-fuzztime` defaults to forever. Give it a value unless you mean to sit and watch.
*   `-fuzzminimizetime` bounds the shrinking pass that runs after a failure is found. The default is fine; raise it when the reported input is bigger than it needs to be.
*   `-count=1` disables the test cache, which otherwise makes a re-run of the seed corpus a no-op.

Because only one target runs per invocation, a sweep is a loop:

```console
$ for t in FuzzEncryptValueRoundTrip FuzzDecryptValueRejectsGarbage FuzzDecryptAtPathRoundTrip FuzzRekeyAtPathPreservesValue FuzzPathIsDecryptable; do
      go test -mod=vendor -count=1 -run=XXX -fuzz=$t -fuzztime=5m ./src/test/e2e/fuzz/ || break
  done
```

Expect very different throughput between the two kinds of target. The ones that only push strings through a parser run at roughly 85,000 executions per second; the ones that encrypt a value run the Ansible Vault key derivation on every execution and manage a few hundred. Both are useful, but a document-level target needs minutes where a path target needs seconds.

## The corpus

There are two corpora, and only one of them is in the repository.

Inputs the fuzzer generates live in the build cache, under `$(go env GOCACHE)/fuzz/`. They are shared across runs on the same machine, are not portable, and are not meant to be committed; `go clean -fuzzcache` discards them, which is worth doing when a target has been rewritten and its cached corpus is exercising a shape that no longer exists.

Failing inputs are the committed corpus. When a target fails, `go test` writes the input to `src/test/e2e/fuzz/testdata/fuzz/<Target>/<hash>` and prints the path. **Commit that file.** From then on it is replayed by a plain `go test`, which is how a crash found by a long fuzz run becomes a permanent regression test that costs milliseconds. The three currently committed are the defects found when these targets were written:

```
FuzzDecryptAtPathRoundTrip/89831cc049267b2c    string("\n")     a credential of one line break, written as an empty block scalar, read back as ""
FuzzPathIsDecryptable/477bd66902831e69         string("0[0")    an unclosed index, which panicked inside go-yaml instead of erroring
FuzzPathIsDecryptable/ebc8a2cafef15425         string("'$'")    a path whose canonical spelling, "$.$", the parser then rejects
```

Those same failures are also written up as ordinary table cases next to the code they broke -- see `TestEncryptAtPathReadsBackWhatDecryptWrote` and `TestCanonicalYAMLPath` in `src/internal/clustercfg/vaultpath_test.go`. Keep doing both: the corpus file is what stops the target regressing, and the named case with a comment is what explains the defect to the next reader.

## Writing a target

A target is a function taking `*testing.F`, seeded with `f.Add` and run with `f.Fuzz`. The fuzzed arguments may only be `[]byte`, `string`, the integer and float types, `bool`, and `rune` -- no structs, no slices of anything else. Build the structure you need inside the body from those pieces.

```go
func FuzzEncryptValueRoundTrip(f *testing.F) {
	f.Add("hunter2")
	f.Add("")
	f.Add("\xff\xfe not valid utf-8")

	f.Fuzz(func(t *testing.T, value string) {
		encrypted, err := clustercfg.EncryptValue(value, fuzzPassword)
		require.NoError(t, err)

		decrypted, err := clustercfg.DecryptValue(encrypted, fuzzPassword)
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

## Where to look next

Fuzzing pays where a wrong answer still parses, so the targets worth writing next are the places that choose an encoding, canonicalise a string, or hand a path to a parser:

*   `distrocfg.marshalRegistriesYAML` and `quoteRegistryKeys` (`src/types/distrocfg/distro_common.go`) choose a quoting style for keys the engine reparses -- the same shape as the bugs above, over registry names and `rewrite` regex patterns.
*   `split.SplitFile` and `split.ReassembleFile` are a round trip over content bytes and a chunk size, and `ReassembleFile` trusts the `part000` metadata it unmarshals from disk.
*   `cfg.Parse`, `cfg.ParseMultiDoc` and `utils.ReadByteStrict` take document bytes straight into a third-party parser, which is exactly where the panics found so far have come from.
*   `isCleanPathRegex` (`src/cmd/common.go`) is a path sanitiser, so the property is that an accepted path, joined to a base and cleaned, stays under that base.
*   `dns.ParseServiceURL` against `dns.IsServiceURL` is a predicate and its implementation, the differential shape.
