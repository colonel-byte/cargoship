# Why the Galaxy collection pins are resolved by a mage target and not a dependency bot

`ansible/colonel_byte/cargoship/requirements.yml` pins the Galaxy collections the collection's role installs. Those pins have a constraint no other dependency in this repository has: every version must run on the ansible-core the controller actually has, which is 2.16.16 -- what `dnf install ansible-core` resolves to on the `almalinux/10-base` image in `containers/ansible/Dockerfile`, and what a management node inside an airlock runs.

Collections move past that regularly. `community.general` 13.4.0 declares `requires_ansible: ">=2.18.0"`; the newest release usable on 2.16 is 11.4.9, two majors behind. A bot that bumps to latest would pin a collection that cannot load.

## Renovate cannot do it, and the reason is not going to be worked around

Renovate was the obvious candidate: it has an `ansible-galaxy` manager that reads this exact file, which Dependabot does not. It still does not work here.

- `constraintsFiltering: strict`, the mechanism for dropping releases whose declared language support does not match the repository's, covers `crate`, `go`, `jenkins-plugins`, `npm`, `packagist`, `pub`, `pypi` and `rubygems`. `galaxy-collection` is not among them.
- Even where filtering is enabled, it needs the datasource to report a constraint per release. Renovate's `galaxy-collection` datasource reads `created_at` and `repository` out of the Galaxy v3 response and never maps `requires_ansible` onto `release.constraints`. There is nothing to filter on.

So Renovate's manager would bump this file blind. Hand-maintained `allowedVersions` ceilings in `packageRules` were the alternative -- `community.general` capped at `<12`, and so on -- and were rejected: the ceiling is a second copy of a fact Galaxy already publishes, it goes stale silently when a collection raises its floor inside a major, and being wrong looks exactly like being right until a playbook fails on a node.

## Galaxy publishes the data, on the endpoint Renovate already calls

```
GET /api/v3/plugin/ansible/content/published/collections/index/{namespace}/{name}/versions/
-> data[].requires_ansible
```

Every published version carries its specifier. Resolving the pin correctly is then a filter and a max, which is `magefiles/ansible-requirements.go`, driven by two targets:

- `mage generate:ansibleRequirements` pins each collection to the newest release whose `requires_ansible` admits the whole controller range, and names the newest release alongside it when the two differ, so a held-back pin reads as held back rather than current.
- `mage test:ansibleRequirements` checks only the pinned versions and fails when one does not admit that range. `.github/workflows/check-ansible-requirements.yaml` runs it on every pull request.

The split is deliberate. A gate that asserted the pins were the *newest* would fail an unrelated pull request whenever an upstream collection shipped that morning, which trains people to ignore it. The gate asserts the pins are *usable*; moving them forward is a deliberate run of the generator.

This is the same shape as `Generate.UpdatePins` in `magefiles/gen-engine-update-pins.go`, which resolves upstream k3s and rke2 tags into `thirdparty-src/pins.json`. Both exist because a version this repository pins has a correctness condition attached that a generic bot has no way to evaluate.

## `meta/runtime.yml` is the one place the controller range is written down

`requires_ansible` in the collection's `meta/runtime.yml` is both the collection's declared support and the resolver's input. It is a range rather than a floor -- `">=2.16.0,<2.17.0"` -- because a floor alone does not say which ansible-core the pins are resolved for, and a second copy of that number somewhere else would be a thing to keep in step.

It had been `"2.16.16"`, which is not a PEP 440 specifier at all. `SpecifierSet` raises on a bare version, and ansible-core catches that raise in `plugins/loader.py` and downgrades it to `Error parsing collection metadata requires_ansible value`, so the declared floor enforced nothing and said so only in playbook output nobody reads. `controllerRange` therefore treats an unparseable or half-bounded `requires_ansible` as an error rather than a skip, and `TestParseSpecifierRejectsABareVersion` pins that.

The upper bound tracks the base image. Bumping `containers/ansible/Dockerfile` to a release whose `ansible-core` is newer means editing this line and re-running the generator; the pins will move forward on their own once it does.

## Revisit trigger

Renovate adding `galaxy-collection` to `constraintsFiltering` and surfacing `requires_ansible` as a release constraint. At that point the manager plus `constraints.ansible` would do what these two targets do, with PRs for free, and this can be deleted rather than rediscovered. Nothing else about the setup is worth keeping if that happens.
