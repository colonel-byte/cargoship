# Why mirror records live in one repository, tagged by kind

`cargoship mirror` and `cargoship publish --record` both leave a record of what they put in a registry, and `cargoship ls` reads those records back. The layout is one repository -- `cargoship`, under whatever prefix the target carried -- holding one tag per record:

| Tag                     | Written by                     |
| ----------------------- | ------------------------------ |
| `_mirror.NAME.VERSION`  | `cargoship mirror`, by default |
| `_package.NAME.VERSION` | `cargoship publish --record`   |

From the code alone this looks like it could just as well be one repository per package with the version as the tag, which is what it was first. This records why it is not.

## One repository, because discovery has to work where people publish

Records spread across repositories have to be found by asking the registry for its catalogue, and `/v2/_catalog` is a wider permission than pushing wherever it is implemented at all. It is not implemented everywhere:

| Endpoint                                    | ghcr.io          |
| ------------------------------------------- | ---------------- |
| `/v2/_catalog`, with a catalog-scoped token | **`403 DENIED`** |
| `/v2/colonel-byte/cargoship/tags/list`      | `200`, 67 tags   |

Measured against the real registry, not inferred. GitHub does not implement catalogue enumeration and Docker Hub does not either, so the per-package layout could not list a registry on either -- `cargoship ls` worked only against a registry that allowed a catalogue read, which the push credentials Zarf generates frequently do not. One repository makes a listing a single `tags/list`, which every registry serves and which needs no more than a pull.

## Kind in the tag, because two kinds cannot share one

A package both published and mirrored at the same name and version would want the same tag if the kind were not in it, and the second write would silently replace the first. The surviving record would then be wrong about what the registry holds -- claiming an artifact to pull where there is loose content, or the reverse -- which is worse than having no record.

The kind is in the record's JSON as well, and the two serve different purposes: the tag is the index, so a listing can skip a manifest it does not want without fetching it, and the JSON is the authority, so a consumer holding a record never has to parse a reference.

## Not one record carrying both facts

The alternative to separate tags was one record per name and version with flags for what had happened to it. It is rejected for two reasons, and the second is the hard one.

The two records are written at different times by different commands, so merging means read-modify-write: a mirror would have to fetch a publish's record, add to it, and push it back. That makes a mirror's success depend on parsing an artifact it did not write.

And it cannot be done at all on a registry that locks tags. Nexus can be configured to refuse overwriting an existing tag, often with `latest` as the only exception, and a read-modify-write cycle is exactly what such a registry forbids. Separate tags per kind mean the only rewrite is a genuine re-run of the same operation.

## The tag prefix is derived from the kind, not mapped to it

`recordTagPrefix` returns `"_" + kind + "."`. It used to be a `switch` returning `_pkg.` for the kind the JSON called `package`, which meant a consumer reading `kind: package` out of `cargoship ls -o json` and building a reference from it would construct `_package.` and find nothing. Two spellings of one concept, mapped by hand, in a package that already carries an anti-drift test for the same hazard elsewhere. Deriving it makes them incapable of disagreeing, and deletes the mapping and its error branch.

The underscore is what keeps a record clear of a package published into the same repository: a tag may begin with `_` and a version does not, so `cargoship:1.0.0` and `cargoship:_package.rke2.1.0.0` cannot collide. This is convention rather than enforcement -- the schema bounds a version's length but not its characters -- and nothing checks it.

## A failed record write is a warning, not an error

A record is written to a stable tag, so re-running a mirror or a publish for a version that already has one rewrites that tag -- and a registry which locks tags refuses it. Both commands warn and carry on. The alternative, which `mirror` did at first, was to return the error, and that made a mirror which had pushed every byte fail at its last step: the same shape as the ingest-directory defect fixed in [#1114](https://github.com/colonel-byte/cargoship/pull/1114), a long operation that succeeded reporting failure from its tail.

The cost of warning is real and is why the warning says more than the error would: `cargoship ls` is the discovery mechanism, so a record that was not written is a package a consumer cannot find. The warning names that consequence rather than only the underlying failure.

No attempt is made to tell an immutable-tag refusal apart from any other failed push. Doing so means matching on registry-specific status codes and messages, which is a guess that silently stops being true, and the handling would be the same either way.

## What a re-mirror actually sends

Worth recording because it is easy to assume the opposite in both directions. `oras.Copy` checks the destination before pushing and skips a rooted sub-graph that already exists (`vendor/oras.land/oras-go/v2/copy.go`, "skip if a rooted sub-DAG exists"), so re-mirroring a package does not re-upload content the registry already holds. But it also **enforces tagging on the root even when the root was skipped** (same file, "enforce tagging when the skipped node is root"), so a tag request goes out per image on every run regardless.

The consequence for a tag-locking registry is that re-mirroring an unchanged package produces identical digests, and a re-tag to the same digest is usually permitted because it changes nothing; re-mirroring a *rebuilt* package under a version it already used is the case to expect a refusal on. That refusal fails the mirror, and should: an image a node will ask for and not find is a broken mirror rather than a cosmetic problem. Only the record write is advisory.
