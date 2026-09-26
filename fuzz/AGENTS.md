# Fuzz target names in this package

## Don't give two `Fuzz*` functions names where one is a prefix of the other

`go test -fuzz=<value>` matches `<value>` as an unanchored regexp against every fuzz function name in the package. A plain name like `FuzzParseRecipient` has no anchors, so it matches as a substring anywhere it occurs -- including inside `FuzzParseRecipients`, which starts with exactly that string. Running `-fuzz=FuzzParseRecipient` then hits both and `go test` refuses to pick one:

```
testing: will not fuzz, -fuzz matches more than one fuzz test: [FuzzParseRecipient FuzzParseRecipients]
```

```go
// No -- FuzzParseRecipient is a prefix of FuzzParseRecipients, so neither name can be
// targeted alone without anchoring the regexp by hand at the call site.
func FuzzParseRecipient(f *testing.F) { ... }
func FuzzParseRecipients(f *testing.F) { ... }
```

```go
// Yes -- names a single line versus a whole file explicitly; neither is a substring of the other.
func FuzzParseRecipientLine(f *testing.F) { ... }
func FuzzParseRecipients(f *testing.F) { ... }
```

### Why

A one-off collision like this is easy to work around in the moment (`-fuzz='^FuzzParseRecipient$'`), but the workaround has to be remembered every time, by everyone who runs that target -- CI included, wherever a target name is passed through as a flag value rather than typed by hand. Picking non-overlapping names once removes the anchoring requirement permanently, for that target and for whoever adds the next `Fuzz*` sibling to it.

### How

Before adding a `Fuzz*` function whose name shares a stem with an existing one in this package (a singular/plural pair, a `...Line` vs `...File` pair, and so on), check that neither name is a prefix of the other:

```sh
grep -oh '^func Fuzz[A-Za-z0-9_]*' fuzz/*.go | sed 's/func //' | sort
```

If two names would collide, pick the more specific one for the narrower-scoped target -- what it parses (`...Line` vs the reader/file-level function it feeds into), what property it checks, or similar -- rather than shortening the broader one.
