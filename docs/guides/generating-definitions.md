# Generating Definitions From a Template

A fleet's package definitions are usually the same file several times over. A `distro.yaml` for v1.36.4 and one for v1.37.1 differ in a version string and the URLs that carry it; an amd64 definition and an arm64 one differ in an architecture selector; a flannel definition and a cilium one differ in a manifest and a pod CIDR. Kept by hand, the copies drift - a fix applied to three of four definitions is the normal failure, and nothing reports it.

`cargoship dev render` keeps them as one template plus a small values file per definition:

```bash
cargoship dev render --template ./distro.yaml.tmpl --values ./v1.36.4.yaml --out ./packages/v1.36.4
```

This is the same generator cargoship's own examples under [`example/`](https://github.com/colonel-byte/cargoship/tree/main/example) are rendered with, which is why there are no hand-written definitions in this repository either.

## What It Writes

Each `--template` renders to its own base name with the `.tmpl` suffix dropped, into the `--out` directory, which is created if it does not exist. `--template` and `--values` are both repeatable, and `-t`, `-f` and `-o` are the short forms.

| Template                  | Written as           |
| ------------------------- | -------------------- |
| `distro.yaml.tmpl`        | `distro.yaml`        |
| `values.yaml.tmpl`        | `values.yaml`        |
| `values.schema.json.tmpl` | `values.schema.json` |
| `distro.yaml`             | `distro.yaml`        |

A template with no `.tmpl` suffix renders to its own name, so `--out` has to name a directory other than the one the templates are in - rendering a template onto itself is refused rather than silently overwriting it, as is two templates whose output names collide.

Every template in one invocation renders against the same merged values, which is the point of passing more than one. A package's [values file and its schema](package-values.md) sit beside its definition rather than inside it, so a value that has to agree between the two - a chart name the definition maps and the schema enumerates - is written once and used in both:

```bash
cargoship dev render -t ./distro.yaml.tmpl -t ./values.yaml.tmpl -t ./values.schema.json.tmpl \
  --values ./common.yaml --values ./v1.36.4.yaml -o ./packages/v1.36.4
```

## The Values Files

A values file is YAML holding a mapping, and it reaches the template as the template's own context - not under `.Values`. A file setting `version: 1.36.4-k3s1` is addressed as `{{ .version }}`.

Files are merged in the order given, with later files winning, and nested mappings are merged rather than replaced. So a shared file carries what every definition agrees on and a per-definition file carries what it does not:

```yaml
# common.yaml
flavor: flannel
registry: registry.example.com
images:
  pause: mirrored-pause:3.6
```

```yaml
# v1.36.4.yaml
version: 1.36.4-k3s1
binarySHA: 0f7a...
```

```bash
cargoship dev render -t ./distro.yaml.tmpl --values ./common.yaml --values ./v1.36.4.yaml -o ./packages/v1.36.4
```

A values file must be `.yaml` or `.yml`, and its top level must be a mapping - values are addressed by key, and a document that is a list or a bare scalar has no keys to address. An empty file is fine.

## What a Template May Call

The template functions are the ones a package's own values templates get: the [sprig](https://masterminds.github.io/sprig/) set, plus Helm's serialization helpers, minus the functions that read host state. `upper`, `printf`, `default`, `toYaml`, `range` and the rest are all available.

| Not available                                     | Why                                                                                                                                                                                                              |
| ------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `env`, `expandenv`, `getHostByName`               | A definition that reads the generating machine's environment or DNS produces a different package on the next machine. Put the value in a values file instead                                                     |
| `fileExists`, `downloadToCache`, `cachedFilePath` | These belong to a package's `onCreate` actions, which run at build time. A render touches no network and reads nothing but its own inputs                                                                        |
| `sha256`, `sha256member`                          | The digest helpers the examples under `example/` use fetch and hash remote artifacts against a committed cache. They are part of this repository's generator, not of `dev render` - put digests in a values file |

A function a template names and the renderer does not have is a parse error, so a template reaching for one of these fails loudly rather than rendering an empty string.

## Emitting Template Syntax Into the Definition

A `distro.yaml` can itself carry `{{ .Values.x }}` expressions, which cargoship resolves at build time against the package's values. Those braces have to survive the render that produces the definition, so write them as a quoted string the generator substitutes:

```yaml
# in distro.yaml.tmpl
  cilium:
    l2announcements:
      enabled: {{ "{{ .Values.cilium.l2announcements.enabled }}" }}
```

```yaml
# in the rendered distro.yaml
  cilium:
    l2announcements:
      enabled: {{ .Values.cilium.l2announcements.enabled }}
```

The same applies to a `{{- if }}` block meant for build time. This is what the shipped example templates do - see `rke2-distro.yaml.tmpl` in [`magefiles/templates/`](https://github.com/colonel-byte/cargoship/tree/main/magefiles/templates) for a flavor that drives a chart from values this way.

## Mistakes It Catches, and One It Does Not

A value path that resolves to nothing is an error, not a placeholder in the output. Go templates print `<no value>` for a key the values do not set, which would otherwise ship a definition carrying that literal string:

```console
$ cargoship dev render -t ./distro.yaml.tmpl --values ./v1.36.4.yaml -o ./out
ERR rendering ./distro.yaml.tmpl: template "..." resolved a missing key to "<no value>"
```

An output named `distro.yaml` is then checked against the package schema, and **nothing is written unless it passes** - so a failed render leaves no half-finished definition on disk to be mistaken for a good one:

```console
$ cargoship dev render -t ./distro.yaml.tmpl --values ./broken.yaml -o ./out
ERR distro.yaml does not match the package schema:
  spec.config: Additional property images is not allowed
```

That is the same check [`cargoship validate`](../commands/cargoship_validate.md) applies to a definition already on disk, read from the schema the binary carries, so it needs no network. Pass `--no-validate` to write the output anyway, which is what to reach for while a template is half-written.

The limit worth knowing: the missing-key check sees what the template *printed*. A missing value passed through a function is formatted by that function rather than printed as `<no value>`, so `{{ printf "%s-k3s1" .verison }}` renders `%!s(<nil>)-k3s1` and is caught by the schema check or not at all. Address values directly where you can, and read the first definition a new template produces.

## A Release Line in One Loop

Because the inputs are files, a line of releases is a shell loop and nothing more:

```bash
for v in 1.36.3-k3s1 1.36.4-k3s1 1.37.1-k3s1; do
  cargoship dev render -t ./distro.yaml.tmpl -t ./values.yaml.tmpl \
    --values ./common.yaml --values "./versions/$v.yaml" -o "./packages/$v"
done
```

Each directory is then a package source `create` takes:

```bash
cargoship create ./packages/1.36.4-k3s1
```

Re-rendering is safe and is how a template fix reaches every definition: the outputs are overwritten in place, so running the loop again after editing the template leaves the tree consistent. Nothing is deleted - a definition whose values file is gone stays on disk until it is removed by hand.

## Where to Go Next

- [Rendering Definitions in CI](rendering-in-ci.md) - wiring the render into a GitHub Actions workflow that re-renders on a schedule and opens a pull request with whatever moved
- [Package Values and Templating](package-values.md) - what the `{{ .Values.x }}` expressions in a rendered definition can do at build time, and how values files and schemas are composed
- [Authoring a Distro Package](../dev/package-authoring.md) - the edit loop around a definition, and `dev apply` for testing one without building a package
- [`cargoship dev render`](../commands/cargoship_dev_render.md) - the generated flag reference
- [`example/AGENTS.md`](https://github.com/colonel-byte/cargoship/blob/main/example/AGENTS.md) - how this repository drives the same generator over a matrix of distros, flavors and release tags
