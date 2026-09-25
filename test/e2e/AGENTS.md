# Test data in the e2e suites

## Put test documents in files, not in Go string constants

A YAML document a test encrypts, applies, or parses belongs in a file under a `testdata/` directory, read at the top of the test. It does not belong in a `const doc = ` + backtick string in the `_test.go` file beside it.

```go
// Yes.
func TestEncryptConfig(t *testing.T) {
	doc := readVaultInventoryFixture(t)
	got, changed, _, err := EncryptConfig(doc, testKeyring, false)
```

```go
// No.
const configTestDoc = `apiVersion: zarf.dev/v1alpha1
kind: ZarfCluster
...
`
```

This applies to the unit tests as well. `src/internal/clustercfg` reads its fixtures from `test/e2e/noncluster/testdata` rather than keeping a second copy of the same inventory, so a change to what an inventory looks like lands in one place and both suites see it.

### Why

- **One document, one copy.** The same inventory is usually walked by a unit test and by the e2e test that runs the real binary over it. Two copies drift, and the drift shows up as a test that passes over a document no operator writes.
- **A fixture is checked against the schema.** A file under `testdata/` carries a `# yaml-language-server: $schema=...` header, so an editor flags a field the schema does not accept. A Go string is checked by nothing, and fixtures written that way have carried fields the schema rejects.
- **Indentation survives.** A backtick string sits at column 0 inside an indented function, so YAML in it reads badly and is easy to get wrong. A file is just the file.
- **The document is diffable.** A reviewer reading a change to a fixture sees a YAML diff rather than a diff of a Go literal, and `git blame` on the fixture answers why a field is there.

### How

Add the document to the `testdata/` directory of the suite that uses it, with a header that says what shape it covers and why, ending in a line stating that nothing in it is contacted:

```yaml
---
# yaml-language-server: $schema=../../../../../schema/zarf-v1alpha1-cluster-schema.json
# What this document covers and why a test needs that shape.
#
# Nothing here is contacted. The tests only read this file and rewrite copies of it.
apiVersion: zarf.dev/v1alpha1
kind: ZarfCluster
```

Then read it through a helper that fails the test rather than returning an error:

```go
const vaultInventoryFixture = "../../test/e2e/noncluster/testdata/inventory-vault.yaml"

func readVaultInventoryFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(vaultInventoryFixture)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", vaultInventoryFixture, err)
	}
	return data
}
```

The two suites resolve that path differently, and the difference is not visible from the constant. A unit test runs with its own package directory as the working directory, so it names a fixture relative to the package: `../../../test/e2e/noncluster/testdata/inventory-vault.yaml`. An e2e test runs from the repository root, so the same fixture is `test/e2e/noncluster/testdata/inventory-vault.yaml`. Copying a constant from one suite into the other gives a file that is not there, reported as a failure to read it.

These commands rewrite the file they are given, so a test that runs one copies the fixture into `t.TempDir()` first and leaves the checked-in file alone.

### Two things to avoid in tests over a fixture

- **Do not count lines to a value.** `const replaced = 10 // the "pass:" line` breaks the next time somebody adds a comment to the fixture, and it breaks as though the code under test moved something. Find the line by its prefix instead.
- **Do not assert on a count where the paths themselves can be named.** `len(changed) != 8` passes when the wrong eight values were rewritten. Write out the paths the walk is expected to return, and give any `switch` over fixture contents a `default` that fails, so a renamed field in the fixture is a failure rather than a set of assertions that quietly stop running.

## When a document does belong in the Go file

Only when the document is not a document: a few bytes that are deliberately malformed, or a single scalar. A parse-error case that is two lines long is clearer beside the assertion than in a file of its own.

A valid document that a test then mutates is not one of these. Read the fixture and mutate the copy, the way `TestEncryptConfigRejectsAnAliasWithoutAnAnchor` does:

```go
fixture := readAnchoredInventoryFixture(t)
doc := strings.Replace(string(fixture), "auth: &robot-auth", "auth:", 1)
```
