### Is your feature request related to a problem? Please describe.

`zarf package create` decides how to open an archive from its **file name** and nothing else. [`findArchiver`](https://github.com/zarf-dev/zarf/blob/1f516c0fce0c/src/pkg/archive/archive.go#L100-L111) walks a map of extensions and refuses anything that does not end in one:

```go
func findArchiver(name string) (archives.Archiver, error) {
	var best string
	for ext := range archivers {
		if strings.HasSuffix(name, ext) && len(ext) > len(best) {
			best = ext
		}
	}
	if a, ok := archivers[best]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("unsupported archive extension for %q", name)
}
```

So a component file whose `extractPath` points into a perfectly ordinary gzipped tar fails whenever the name it was fetched under does not carry `.tar.gz`. That happens in ordinary places:

- a release URL that ends in a query string, or serves the name through `Content-Disposition`
- a download endpoint with no extension at all, which several vendors use for a "latest" redirect
- any archive fetched into a temporary file whose name is chosen by something other than the source

The error names a path the component's author never wrote, and says the extension is unsupported when the real answer is that there is no extension to read.

### Describe the behavior you'd like

- **Given** an archive whose name carries no extension this package recognises
- **When** `archive.Decompress` is asked to open it
- **Then** the format is identified from the bytes, and only an archive that is genuinely unidentifiable is refused

[`mholt/archives`](https://github.com/zarf-dev/zarf/blob/1f516c0fce0c/go.mod) is already a direct dependency and already exports exactly this:

```go
func Identify(ctx context.Context, filename string, stream io.Reader) (Format, io.Reader, error)
```

It takes the name *and* the stream, matching on the name first and sniffing the content when that fails, and it returns a rewound reader so nothing is consumed by the check. The change is to fall back to it where `findArchiver` currently returns its error, leaving the extension path exactly as it is so no currently-working archive takes a different route.

This is additive: an input that works today still works and still resolves by extension; an input that fails today may now succeed. Nothing is deprecated and no signature changes.

### Describe alternatives you've considered

**Leave it, and document that sources need extensions.** This is defensible - the current behaviour is a refusal rather than a corruption, and it is cheap to work around by renaming. It loses on the cases where the name is not the author's to choose, which is most of the ones above.

**Sniff instead of matching on the extension.** Simpler to describe, but it changes the resolution order for every archive that works today, and content sniffing is ambiguous for formats that share magic bytes. The fallback keeps today's answers and only adds answers where there are none.

**Let a component declare the format.** A new field, to say something the bytes already know. It also has to be plumbed through both API versions, which is a large cost for a narrow case.

### Additional context

The failure surfaces as `unsupported archive extension for "/tmp/.../tmp123"` - a temporary path, so a reader cannot tell which component file caused it. Whatever is decided about the fallback, naming the source alongside the temporary path would make the error actionable on its own.
