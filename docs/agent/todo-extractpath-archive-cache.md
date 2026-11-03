# TODO: persist archives fetched for an `extractPath` file

This is a punch list, not a decision record like the `choice-*.md` files beside it. Every walk of the cluster suite builds its own package, each `distro.yaml` names an `etcd` release to pull `etcdctl` out of, and none of those archives is ever cached between runs - so a cluster walk downloads them again every time and a momentary DNS failure fails the walk at the package build. That accounted for three failed runs out of five in one session, at `Test_00_UpgradePackage` and `Test_00_JoinMachine`, neither of which has anything to do with the phases under test.

## Why they are not cached

`assembleFile` in [`pkg/packager/assemble/assemble.go`](../../pkg/packager/assemble/assemble.go) splits on whether a file declares `extractPath`:

| Entry              | Download call                                                      | Persisted?                           |
| ------------------ | ------------------------------------------------------------------ | ------------------------------------ |
| plain `source`     | `DownloadToFileWithChecksum(ctx, source, dst, file.Shasum, ...)`   | yes - keyed by shasum under `files/` |
| with `extractPath` | `DownloadToFileWithChecksum(ctx, source, compressedFile, "", ...)` | no - `archives` is per-build only    |

The empty shasum is deliberate and correct: with `extractPath` set, `file.Shasum` is the digest of the *extracted* file, and the archive it came out of has a different one. The `archives` map it uses instead is reserved and populated within a single assemble call, and the `defer` removes an archive whose download failed. Nothing writes it to `~/.cache/cargoship/files`, so the next build starts from nothing.

The evidence is the cache itself: 1.7G and 109 entries under `files/`, every one an RPM or a `.deb`, and `find ~/.cache/cargoship -iname '*etcd*'` returns nothing after five full runs that each downloaded one.

## What to do about it

The archive needs a cache key that does not depend on a digest the definition does not carry. Two shapes, in preference order:

- **Let a `files` entry declare the archive's own digest** alongside the extracted file's, and key the persistent cache on it. This keeps the integrity guarantee that the plain-source path has and is the only option that lets a cached archive be trusted rather than merely reused.
- **Key on the resolved URL and verify the extracted file as today.** Cheaper and needs no schema change, but a cached archive is then trusted on its name - acceptable for the suite, weaker than what this project does everywhere else, and worth saying out loud in the code rather than leaving a reader to notice.

Do not reach for a suite-local pre-warm step that downloads the five `etcd` versions before the walks. It hides the problem from the one place that keeps hitting it, leaves a real `cargoship package build` re-downloading on every invocation, and the examples drift - the versions in use today are v3.6.6, v3.6.7, v3.6.12, v3.6.14 and v3.7.1, and nothing would keep that list in step.

## Scope

This is about the archive, not about the extraction or the checksum of what comes out of it, both of which work. Nor is it about node egress - see [`todo-microvm-egress-blackout.md`](todo-microvm-egress-blackout.md); a package build legitimately reaches the network, and the point here is that it should not have to do so twice for the same artifact.
