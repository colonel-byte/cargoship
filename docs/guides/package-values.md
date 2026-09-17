# Package Values

A cargoship package ships with a set of values: Helm-style configuration, addressed by dotted paths, that a cluster can override without rebuilding the package. Values reach the engine configuration in two ways -- structural *mappings*, and Go *templates* that read `.Values` -- and they are also available to the files a package uploads and to the actions that build it.

This guide covers authoring values in a package, overriding them from a cluster inventory or the command line, and choosing between mappings and templates.

## Declaring values in a package

Values live under `spec.values` of a `distro.yaml`:

```yaml
spec:
  values:
    files:
      - values.yaml
      - overrides/prod.yaml
    schema: values.schema.json
```

`files` are merged in order, key by key, with a later file winning over an earlier one. Each entry is a path relative to the package definition, an absolute path, or a URL. Cargoship copies them into the package as `values/<index>-<basename>`, so two files that share a base name do not collide.

`schema` is a JSON Schema document the merged values must satisfy. Cargoship checks the values against it while building the package and refuses to build when they do not match, so a typo is caught by whoever builds the package rather than by whoever installs it. The schema must not use `$ref`. Set `"additionalProperties": false` on the root object: without it, a misspelled top-level key in a cluster inventory is silently ignored instead of rejected.

A values file is ordinary YAML:

```yaml
cilium:
  encryption:
    enabled: false
  hubble:
    ui:
      enabled: false
      ipv6: false
```

## Overriding values

Values are resolved from three sources, each winning over the one before it:

1. The values files the package ships with.
2. `spec.config.values` in the cluster inventory.
3. One or more `--values <file>` flags on the command line.

The inventory carries the values a cluster always wants:

```yaml
kind: ZarfCluster
spec:
  config:
    values:
      cilium:
        encryption:
          enabled: true
```

The flag carries the ones that change between runs:

```sh
cargoship apply ./package.tar.zst --config inventory.yaml --values ./encryption-off.yaml
```

`--values` may be given more than once, with a later file winning over an earlier one, and it is accepted by `cargoship apply`, `cargoship prepare`, and `cargoship engine-config-sync`. There is no `-f` shorthand: `apply` already binds `-f` to `--update-fapolicyd`.

Merging is per key, not per document. An override that sets `cilium.encryption.enabled` leaves every other key under `cilium` as the package defined it.

The merged values are checked against the package's schema again at install time, so an inventory that sets a key the package does not accept fails before anything is written to a host.

## Mappings

A mapping projects one value onto one place in the engine configuration:

```yaml
spec:
  values:
    mappings:
      - source: .cilium.encryption.enabled
        target: .manifest.rke2-cilium.encryption.enabled
```

`source` is read out of the resolved values. `target` is written relative to `spec.config.engine`. Both paths must begin with a dot. A source the values do not define is left alone, so the package's own engine configuration stands as the default.

Mappings are structural: the value keeps its YAML type, and nothing in the package has to be written as a template. They are the right tool when a value lands on exactly one key.

A mapping that cannot be applied -- a path without a leading dot, or a target underneath a chart entry that is a YAML string rather than a map -- is reported as a warning when the package is built, and again when it is installed. It does not fail the build. The feature is new, and a hard failure on a package that installs correctly today is the wrong first move.

## Templates

A template reads `.Values` directly, anywhere in the engine configuration:

```yaml
spec:
  config:
    engine:
      manifest:
        rke2-cilium:
          encryption:
            enabled: "{{ .Values.cilium.encryption.enabled }}"
```

A scalar that is *entirely* one template is re-typed after rendering, the same way Helm's `--set` typing works, so the example above lands in the generated `HelmChartConfig` as a YAML boolean rather than the string `"true"`. A scalar that mixes text and templating stays a string.

Rendering happens before the mappings are applied, and before the engine configuration is validated. Package-authored text is therefore what gets executed: a value supplied by a cluster inventory that happens to contain `{{` is written through as literal text, never evaluated.

### Conditionals

Templating can do the one thing mappings cannot: decide whether a block of configuration exists at all. Rendering walks the parsed structure one scalar at a time and never re-parses a document, so a conditional only works inside a chart entry written as a YAML block scalar:

```yaml
spec:
  config:
    engine:
      manifest:
        rke2-cilium: |-
          kubeProxyReplacement: true
          encryption:
            enabled: {{ .Values.cilium.encryption.enabled }}
          {{- if .Values.cilium.hubble.ui.enabled }}
          hubble:
            enabled: true
            ui:
              enabled: true
          {{- else }}
          hubble:
            enabled: false
          {{- end }}
```

Both shapes are in the examples: `example/rke2-cilium-vsphere` spends its values through mappings, and `example/rke2-multi-cni-cilium` spends the same values through a templated block scalar.

### Which to reach for

Use a mapping when a value has one destination and its type should survive untouched. Use a template when one value drives several keys, when the value needs reshaping on the way, or when the package has to emit or omit a whole block. A package may use both.

## Disabling bundled addons

RKE2 and K3s both install a set of charts of their own -- ingress, a load balancer, metrics, DNS -- and both are told to leave one out through a single `disable` key in `config.yaml`. Every example package exposes that key as a value, so which addons a cluster runs is a cluster's decision rather than a packaging one:

```yaml
# values.yaml, in the package
addons:
  disabled:
    - rke2-ingress-nginx
```

```yaml
# distro.yaml, in the package
spec:
  values:
    mappings:
      - source: .addons.disabled
        target: .config.disable
```

An inventory names the charts it wants left out:

```yaml
spec:
  config:
    values:
      addons:
        disabled:
          - rke2-ingress-nginx
          - rke2-traefik
          - rke2-traefik-crd
```

**The list is replaced, not merged.** Values merge per key, and a list is one value, so an inventory that sets `addons.disabled` decides the whole list. Repeat the entries the package disables by default -- `rke2-ingress-nginx` above -- or they come back.

This works because a mapping copies the value as it stands, lists included. Templating cannot produce a list: rendering re-types a fully templated scalar into a boolean, a number, or a string, never into a sequence. A value that has to reach the engine as a list has to arrive through a mapping.

The names are the chart names the engine itself uses:

| Engine | Charts it installs unless disabled |
| --- | --- |
| RKE2 | `rke2-coredns`, `rke2-ingress-nginx`, `rke2-metrics-server`, `rke2-snapshot-controller`, `rke2-snapshot-controller-crd`, `rke2-snapshot-validation-webhook`, and `rke2-traefik` with `rke2-traefik-crd` on the builds that carry them |
| K3s | `coredns`, `servicelb`, `traefik`, `local-storage`, `metrics-server` |

A name neither engine knows is accepted and does nothing, which is what keeps one inventory usable across engine versions that ship different sets.

Two other things follow from disabling a chart:

- **No `HelmChartConfig` is written for it.** A package may configure a chart under `spec.config.engine.manifest` and still have it disabled by an inventory. Cargoship skips the manifest for a disabled chart rather than configuring something that will not be installed, and a file left over from an earlier run is removed the next time the node is synced.
- **Agents are unaffected.** `disable` is a server-only key. It is dropped from an agent's `config.yaml` along with every other controller-only key, and only controllers carry chart manifests in the first place.

## What renders

Templating is deliberately not applied everywhere. Three surfaces render:

- **`spec.config.engine`** -- always, without an opt-in. This is the subtree that becomes the engine's configuration and its `HelmChartConfig` manifests.
- **`spec.config.files` and `spec.config.os.files`** -- per file, when the entry sets `template: true`. It defaults to false, because most files are binaries or archives that a template pass would corrupt.

  ```yaml
  spec:
    config:
      files:
        - source: audit-policy.yaml
          target: /etc/rancher/rke2/audit-policy.yaml
          template: true
  ```

  The file is rendered once, in place in the extracted package, before any phase uploads it.
- **`spec.actions.onCreate`** -- per action, when the action sets `template: true`. This renders at build time, on the machine building the package, and covers the action's `cmd` and its `wait` block.

`spec.config.os.sysctl`, `spec.config.os.environment`, and `spec.config.os.fapolicyd` do not render.

A template that names a value the package does not define is an error, not an empty string, on every surface.

## Rendered manifests on a running cluster

The `HelmChartConfig` manifests a template produces are written to the engine's manifest directory, which the Rancher engines re-read on their own. Cargoship marks them as not requiring a restart: `cargoship engine-config-sync` compares them on disk, writes the drift in place, and logs the paths it wrote. No node is drained for a values change that lands only in manifests.

## Available functions

Templates are rendered with the sprig-compatible function set from [sprout](https://github.com/go-sprout/sprout), which is what a Helm chart author already expects: `toYaml`, `upper`, `b64enc`, `default`, `quote`, the arithmetic and list helpers, and the rest.

Four functions are removed: `env`, `expandEnv`, `expandenv`, and `getHostByName`. A package author writes the templates and an operator runs them, so leaving these in place would let a package read the operator's environment into chart values, or resolve names against the operator's DNS. Helm removes the same four for the same reason. They are removed rather than stubbed, so a package that uses one fails to parse instead of quietly rendering an empty string.

Five functions Helm defines and sprig itself lacks are added back: `toToml`, `fromToml`, `toYamlPretty`, `fromYamlArray`, and `fromJsonArray`. As in Helm, these report a parse failure in band -- in the returned value -- rather than failing the render.

### Create-only functions

Four more functions are available to `onCreate` actions, and only there:

| Function | Purpose |
| --- | --- |
| `fileExists <path>` | Whether a path exists on the machine building the package. |
| `cachedFileExists <relative path>` | Whether a file exists under cargoship's cache directory. |
| `cachedFilePath <relative path>` | The absolute path of a file in cargoship's cache directory. |
| `downloadToCache <url> <relative path> [sha256]` | Downloads a URL into the cache and returns the local path. |

Pulling a missing artifact into the cache is the whole point of a create action, so these belong there. They are not available to values, manifests, files, or anything that runs at install time: a template that renders chart values should not be able to probe the deploy host or issue HTTP requests. A template that names one outside an action fails at parse time.

Each of these returns its error rather than a zero value, so a cache directory that cannot be read stops the build instead of reading as "the file is not there" and sending the package down the wrong branch.

Because the create-time function set is cargoship's own, cargoship -- not the vendored Zarf action runner -- renders `onCreate` actions. Zarf still *executes* them, with its shells, retries, timeouts, and output capture unchanged, and `.Constants` and `.Variables` still resolve. Actions are rendered one at a time, in order, so a variable one action sets is readable by the next.

```yaml
spec:
  actions:
    onCreate:
      before:
        - description: pull the chart
          template: true
          cmd: |
            {{- if not (cachedFileExists "charts/cilium.tgz") }}
            curl -sSLo {{ cachedFilePath "charts/cilium.tgz" }} {{ .Values.cilium.chartURL }}
            {{- end }}
```

One consequence of resolving values before the build runs: an `onCreate` action cannot produce a values file that the same build then reads. Values are resolved, schema-checked, and packaged before the first action runs.

## Verifying a package

Build the package and read the rendered output back:

```sh
cargoship create ./example/rke2-multi-cni-cilium/v1_36/v1.36.4-rke2r1
cargoship apply <package> --config inventory.yaml --dry-run
```

Values are resolved, the schema is checked, and every surface is rendered before the first phase runs, so a bad value or a template that names a key the package does not define fails ahead of any change to a host. A dry run stops there.
