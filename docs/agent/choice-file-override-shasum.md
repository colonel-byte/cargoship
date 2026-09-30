# Why a file override refuses a file with no shasum

`--file-override` redirects the file downloads a distro definition declares to an internal mirror or to a directory of pre-staged assets. If the file it applies to declares no `shasum`, `create` fails and names the file instead of downloading it. The restriction looks like an arbitrary inconvenience from the flag's side, because nothing about a URL rewrite requires a checksum to work, and it will keep looking that way until someone reads why it is there.

## The override is the only thing that makes the checksum load-bearing

Without an override, a file source is a URL that whoever wrote the distro definition chose, pointing at a host they chose. A missing `shasum` there is sloppy, but the trust is the same trust you already placed in the definition.

An override moves that decision to whoever runs `create`. The bytes now come from somewhere the definition's author never named and cannot see -- an Artifactory instance, a Nexus, a directory somebody populated by hand last quarter. The declared `shasum` is the one thing that still ties what lands in the package to what upstream published. If it is absent, the override is an unauthenticated substitution, and the resulting package carries no evidence that it contains what it claims to.

That is a bad trade to make silently, and it is worse because it fails open: the package builds, the cluster comes up, and nothing distinguishes it from a package built against upstream.

## Why not just warn

A warning is what the code used to do about a checksum mismatch, and it is the reason this restriction had to be added at all. The two file loops in `AssembleDistro` logged every `fileGrabber` error and continued, so a mismatch cost one line in a build log and shipped the bad file anyway. Anyone who trusted the "checksums protect you" story was wrong for as long as that was true.

The loops now return the error. Adding an override feature on top of a warning would have recreated the same hole one level up.

## Where this bites, and what to do about it

The failure mode you will actually hit is an internal distro definition that never bothered with `shasum` because it always downloaded from a host the team controlled. Adding an override to it fails immediately, on the first file.

The fix is to add the upstream checksums to the definition, not to relax this. That is work, but it is work that makes the definition better whether or not an override is ever used: the same checksums catch a truncated download, a silently re-tagged release, and an upstream artifact rebuilt in place.

If some future case genuinely needs an unverifiable overridden download, it needs an explicit opt-out flag with a name that says what it gives up, and a line in the build log every time it fires. It does not need this check quietly deleted.

## See also

- `pkg/fileoverride` -- the matcher, and why it parses URLs instead of comparing string prefixes.
- [`docs/guides/file-override.md`](../guides/file-override.md) -- the user-facing guide.
