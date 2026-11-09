### Is your feature request related to a problem? Please describe.

A component file's `source` can be an HTTP URL or a path. It cannot be an OCI reference, so content already published to a registry has to be staged out of it by hand before `zarf package create` can take it.

That is increasingly where this content lives. Zarf publishes its own init package that way: `ghcr.io/zarf-dev/packages/init:v0.89.0` is an image index over three manifests, and each one carries its components as titled layers - `components/zarf-injector.tar`, `components/zarf-registry.tar` and so on. Taking one file out of one of those today means pulling the artifact with another tool, unpacking it, and pointing a component at the result.

**It is also accepted and then mishandled, which is worse than refusing it.** [`helpers.IsURL`](https://github.com/zarf-dev/zarf/blob/1f516c0fce0c/src/pkg/helpers/url.go) tests only for a scheme and a host, so `oci://ghcr.io/org/thing:v1` parses as a URL, [assemble takes the HTTP branch](https://github.com/zarf-dev/zarf/blob/1f516c0fce0c/src/pkg/packager/assemble/assemble.go#L473), and the reference reaches `utils.DownloadToFile`. The failure is an HTTP error about an unsupported scheme rather than anything that says a file source does not take OCI references.

Whichever way this request goes, that second part should be settled: either `oci://` resolves, or lint rejects it with a message that names the problem.

### Describe the behavior you'd like

- **Given** a component file whose `source` is an `oci://` reference
- **When** `zarf package create` stages it
- **Then** the artifact is resolved and the file is taken out of it, with no pre-staging

```yaml
components:
  - name: tools
    files:
      - source: oci://ghcr.io/zarf-dev/packages/init:v0.89.0
        layer: components/zarf-injector.tar
        extractPath: zarf-injector/files/0/zarf-injector
        target: /usr/local/bin/zarf-injector
        executable: true
```

**A new field is the part that needs deciding, because two artifact shapes are in use and one field cannot serve both.**

| Shape                                          | Example                                        | What names the bytes                                   |
| ---------------------------------------------- | ---------------------------------------------- | ------------------------------------------------------ |
| Every file in the release as its own titled layer | `ghcr.io/cri-o/bundle`                       | The layer's `org.opencontainers.image.title`          |
| Layers that are themselves archives            | `ghcr.io/zarf-dev/packages/init`             | The title picks the archive, `extractPath` what is in it |

If `extractPath` alone had to mean both, it would be a path inside an archive for one artifact and a layer title for the other, decided by what the registry happened to serve - so a reviewer could not tell which from reading the component. A separate `layer` field keeps each meaning to itself and lets the two compose, which is what the zarf init package needs.

Resolving a reference that is an index needs an architecture, and that is worth saying explicitly: matching on the architecture alone is not enough, because the platform OS is spelled at least three ways in artifacts that exist - `linux` by vendors publishing binaries, `multi` by zarf's own package indexes, and empty by some artifacts that record only an architecture. Anything that hardcodes one of those cannot read the others; [`oci.PlatformForArch`](https://github.com/zarf-dev/zarf/blob/1f516c0fce0c/src/pkg/oci/common.go#L111) pins `multi`, which is right for a zarf package and matches nothing in a vendor bundle.

### Describe alternatives you've considered

**Leave it, and let people stage the content first.** It works, and for a genuinely disconnected build it is the right flow regardless. It is pure overhead on a build host that has credentials and a route, and it moves the artifact's provenance out of the component and into somebody's shell history.

**Reject `oci://` at lint time and stop there.** Much smaller, and strictly better than today's HTTP error. Worth doing on its own merits even if the rest is declined - the two are independent.

**Infer the shape instead of adding a field.** Try `extractPath` as a layer title first, and treat a sole layer as an archive otherwise. It needs no API change and it reads both artifacts above. It was rejected on the grounds that the field would then mean different things depending on what the registry served, with nothing in the component to say which - and a vendor retitling their layers between releases would move a component silently from one branch to the other.

### Additional context

**Timing.** The field lands in `v1alpha1` and `v1beta1`, so this is materially cheaper before v1.0.0 than after. That is the only reason to look at this one early; the rest of it can wait for whenever someone has time.

**Not a new endpoint concept.** Credentials, TLS, plain-HTTP and registry rewriting are configured already for the registry paths this project has, so a reference in `source` should resolve through the client that exists rather than acquiring a second configuration surface of its own.

**Checksums.** A component file already declares one, and for an `extractPath` entry it is the digest of the extracted member rather than of the archive. That stays true here, and an OCI layer additionally carries its own digest, which the registry verifies on fetch - so content reached this way is verified in transit whether or not the component declares anything.
