# A content store is trusted for a digest-pinned source, and need not be

`fileGrabber` refuses store content for a file that declares no shasum, because a store holds bytes whoever wrote the definition never saw. One case is let through: a source that names a digest, `oci://registry.example.com/charts/my-app@sha256:...`.

That exception is a trust decision rather than a verification, and the distinction is easy to lose:

- **For a registry fetch the digest enforces itself.** The fetch asks the registry for that digest and the transport checks the bytes against it, so a mirror serving something else fails whatever the mirror intended. That is why `--registry-override` is allowed on a digest-pinned source with no shasum.
- **For a content store it does not.** `haulerstore` indexes an artifact by the `original-ref` annotation on its manifest - a string the store's author wrote - and `Store.Fetch` reads the layer named by a descriptor from the store's own manifest. oras verifies the layer against *that* descriptor, so the read is internally consistent and nothing ever compares what the store holds to the digest the definition named. A store could carry an artifact annotated with any reference at all.

Nothing is currently broken by it: a chart pinned by digest carries no shasum by design, so before the exception the combination was refused outright and no working configuration relied on the refusal. But the asymmetry should not survive on the strength of a comment.

## What the check would be

The digest in an `oci://` reference is the artifact's *manifest* digest, and the store has the manifest. So:

1. Keep the manifest descriptor on `haulerstore.file` beside the layer descriptor - the index pass in `Store.index` already holds it where `file` is built.
2. Expose it through `contentstore`, so a caller can ask what manifest the store matched.
3. In `fileGrabber`, where `pinnedByDigest(file.Source)` is true, compare that manifest digest to the one in the reference and refuse on mismatch.

Then the layer is verified against the manifest by oras, the manifest is verified against the definition by the comparison, and the exception is a guarantee rather than a trust decision. It is a small change; it is separate from the chart work only because the chart work is what made the exception necessary.
