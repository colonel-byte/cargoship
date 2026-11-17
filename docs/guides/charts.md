# Deploying a Helm chart from a package

A package can carry a Helm chart and have the cluster install it. `.spec.config.charts` is the section, the Rancher engines' helm controller is what installs it, and the chart tarball travels in the package like any other file - so nothing reaches for a chart repository on a network that has none.

```yaml
spec:
  type: rke2
  config:
    charts:
      - name: my-app
        source: charts/my-app-1.2.3.tgz
        targetNamespace: my-app
        createNamespace: true
        when: .myApp.enabled
        images:
          - registry.example.com/my-app:1.2.3
        values: |
          replicas: {{ .Values.myApp.replicas }}
```

That writes one `HelmChart` resource into the engine's manifest directory on every controller, carrying the chart itself:

```yaml
apiVersion: helm.cattle.io/v1
kind: HelmChart
metadata:
  name: my-app
  namespace: kube-system
spec:
  chartContent: H4sIAAAAAAAA/...   # the tarball, base64
  targetNamespace: my-app
  createNamespace: true
  valuesContent: |
    replicas: 2
```

`chartContent` is the helm controller's own air-gap mechanism: the chart goes to the API server with the resource, and the install job reads it from there. Nothing is served and nothing is fetched, which makes a chart **the one thing a package delivers that never lands on a node's filesystem** - cargoship reads the tarball out of the extracted package on the management node and writes it into the manifest.

### The size ceiling

Because the chart is in the resource, it is in etcd, and base64 is a third larger than the tarball. etcd refuses a value over about 1.5MB by default, so a chart approaching that fails to install with an etcd error nothing connects back to the package.

Cargoship warns at build time above 1MiB of tarball rather than refusing, because the limit is the cluster's to raise (`--max-request-bytes`) and an operator may well have raised it. If a chart is genuinely too large, the options are to trim what the chart carries (vendored CRDs and bundled subcharts are the usual weight) or to deliver it as a `files` entry and apply it another way.

Note that the chart ends up in etcd regardless of how it arrives: the helm controller stores it in the release's own Secret once installed. What `chartContent` adds is one more copy of it, in the resource.

## This is not `engine.manifest`

The two sections look alike and do opposite things.

|                    | `.spec.config.engine.manifest`               | `.spec.config.charts`      |
| ------------------ | -------------------------------------------- | -------------------------- |
| Writes             | `HelmChartConfig`                            | `HelmChart`                |
| Owns the release   | no - the engine bundled the chart            | yes - the package did      |
| Chart comes from   | the engine's own bundle                      | the package                |
| Chart travels      | n/a - the engine already has it              | in the resource, base64    |
| Removing the entry | stops configuring a chart that keeps running | **uninstalls the release** |

That last row is the one to read twice. A `HelmChart` resource owns its release, cargoship prunes the manifest directory against what the package declares, and the helm controller answers a deleted `HelmChart` by uninstalling the workload. The prune only reaches cargoship's own files - every manifest it writes is named `cargoship-<name>-chart.yaml` - so a `HelmChart` a package delivers through `spec.config.files` instead, or one the engine bundles, is left alone. Dropping a chart from a package, or gating it off with `when`, takes the release with it on the next `cargoship apply`. That is the intent - a package is the description of what the cluster runs - but it is destructive, and it is not what dropping a `manifest` entry does.

## Fields

| Field             | What it does                                                                                                             |
| ----------------- | ------------------------------------------------------------------------------------------------------------------------ |
| `name`            | The `HelmChart`'s name, and the stem of the file cargoship writes. Must be unique in the package                         |
| `source`          | Where the chart tarball comes from at build time: a local path, an http(s) URL, or an `oci://` reference                 |
| `version`         | The chart version, which names the tarball when `source` does not. Required for an `oci://` source                       |
| `shasum`          | SHA-256 the tarball is verified against when the package is built. Required for a fetched source                         |
| `targetNamespace` | The namespace the release installs into. The resource itself always lives in `kube-system`, where the controller watches |
| `createNamespace` | Have the helm controller create `targetNamespace` if it is absent                                                        |
| `values`          | The chart's Helm values, as a YAML string or a mapping. Rendered as a Go template first                                  |
| `images`          | The images the chart needs, carried in the package alongside `imageConfig.images`                                        |
| `when`            | A values path gating whether the chart is delivered and deployed at all                                                  |

## What a cluster can change

`values` is rendered as a Go template against the package's values and the cluster's facts, the same way an engine manifest entry's values are - see [package values](package-values.md). That is the one way a cluster configures a chart: `spec.values.mappings` targets are relative to `spec.config.engine`, so a mapping cannot reach a chart entry.

```yaml
# In the package:
values: |
  replicas: {{ .Values.myApp.replicas }}
  ingress:
    host: {{ .Cluster.LoadBalancer }}
```

`when` is how a cluster leaves the chart out entirely: the resource is not written, and the chart's bytes are not even read out of the package. The path has to exist in the package's values and hold a boolean, and only an explicit `false` gates the chart off: a path that is missing, unreadable, or holding anything other than a boolean fails the apply. That is deliberate, and it is the `when` gate's one sharp edge - reading a missing path as off would mean a renamed values key, or an override that replaced the gate's parent map, silently uninstalling a running release.

Note what `when` does **not** gate: the chart's `images` are carried and uploaded either way. The flat image list has no per-value gate to hang them on, which is the same gap [an `enabled: false` chart manifest has](../agent/todo-chart-disable-image-gating.md).

## Where the tarball comes from

`source` takes any of four things, and all four are fetched when the package is **built**, on the management node, never by the cluster:

```yaml
# A file beside the definition. No example ships one - see below - but it is the simplest form,
# and the right one for a chart your own build produced.
- name: my-app
  source: charts/my-app-1.2.3.tgz

# An http(s) URL. A shasum is required - see below.
- name: my-app
  source: https://charts.example.com/my-app-1.2.3.tgz
  shasum: 5f0c5ee6...

# A chart published as an OCI artifact, which is what `helm push` produces. The reference names
# a repository and a tag rather than a file, so `version` is what names the tarball.
- name: my-app
  source: oci://registry.example.com/charts/my-app:1.2.3
  version: 1.2.3
  shasum: 5f0c5ee6...

# The same, pinned by digest, which needs no shasum: the reference is the guarantee.
- name: my-app
  source: oci://registry.example.com/charts/my-app@sha256:5f0c5ee6...
  version: 1.2.3
```

There is deliberately no `repo:` plus `version:` form. The helm controller supports one, and it would have the *cluster* resolve the chart against a repository - which is the one thing an air-gapped cluster cannot do. What reaches a node is always a tarball the package carried.

A chart's tarball is a file in the package, so everything that already applies to a packaged file applies to it unchanged:

- **`--file-override`** redirects an http(s) chart source to a mirror or a local directory, matching on URL prefix like any other file.
- **`--content-store`** resolves a chart out of a hauler store or a zarf package, keyed on the source the definition declared - so a chart synced into a store alongside the rest of a release is read from there rather than fetched. A Helm chart published as an OCI artifact is recognised as a file in such a store, and named the way `helm package` names it, from the chart name and version the manifest records. Note that a store is *trusted* for a source pinned by digest rather than checked against it: see [the note on that](../agent/todo-content-store-digest-check.md).
- **`--registry-override`** rewrites the registry of an `oci://` chart source, the same way it rewrites an image's.
- The **build record** names what was actually read: an overridden or store-resolved chart appears in `build.fileSources` with its declared source, what it resolved to, and the digest it was verified against.

### Why no example carries a chart tarball

`source` taking a local path is the simplest of the four, and the examples deliberately do not use it: a `.tgz` committed to a source tree is bytes nobody reviews, in a format no reviewer can diff, and the xz-utils backdoor was exactly that shape. The two example flavors fetch instead, and each is pinned by a digest a reader can check against upstream.

That is a judgement about what belongs in *this* repository, not a limitation. A local path is right whenever the chart is built by the same pipeline as the package - `helm package` into a directory, point `source` at it, and the tarball never has to be committed anywhere.

### A fetched chart has to name a digest

`shasum` is optional for a local path and required for anything fetched - an `oci://` reference pinned by `@sha256:` excepted, since that is already a digest. A package that leaves it out is refused when it is built rather than carrying a chart nobody verified.

This is stricter than a file entry, which only forces a shasum when an override or a content store redirects it. A chart earns the extra strictness: it installs into the cluster with whatever RBAC and privileges its own templates ask for, so "bytes from a URL nobody checked" is a different proposition than it is for a binary that lands on disk.

## Only the Rancher engines

k3s and rke2 run the helm controller; upstream Kubernetes does not. A package that declares `charts` with `spec.type: upstream` is refused when it is built rather than built with the charts dropped. For upstream, render the chart to plain manifests and use `.spec.config.manifests`, which cargoship `kubectl apply`s from the leader.

## What is already in the package

The helm controller runs each install as a Job using `rancher/klipper-helm`, which both engines already carry in their own image lists - so nothing extra is needed for the installer itself. The chart's *own* images are yours to declare, in `images` above or in `imageConfig.images`.

## Where the chart sits in a built package

Under `charts/<index>/<tarball>`, one directory per entry in declaration order - beside `files/` and `os/`, and staged by the same machinery. It is a layer of the published artifact like any other, so `cargoship publish`, `pull` and `mirror` all carry it.

The one thing it is not is a `files` entry. That is the distinction worth knowing if you go looking: a chart has no `target`, nothing uploads it, and a package that delivered it to every controller as well would be carrying it twice.

## The examples

Three generated examples carry a chart, and they are the shortest way to see a complete definition:

| Example                  | Source                                                        |
| :----------------------- | :------------------------------------------------------------ |
| `example/k3s-chart-http` | `metrics-server`, downloaded and verified against a shasum    |
| `example/k3s-chart-oci`  | `prometheus-node-exporter`, from a registry, pinned by digest |
| `example/k3s-service-lb` | kube-vip plus its cloud provider - two charts in one package  |

## Worked example: kube-vip from its charts

kube-vip is the case worth walking through, because it is the one where the choice between a chart and a static pod is forced rather than a preference, and the answer differs by job.

### Service load balancers - the chart, and two of them

`example/k3s-service-lb` is this, rendered. kube-vip in service mode watches Services of type LoadBalancer and answers ARP for whatever address each one holds; it does not decide the addresses. The `kube-vip-cloud-provider` chart does that, out of a pool it reads from a ConfigMap its own chart renders. Carrying the first without the second installs a controller that advertises nothing and leaves every Service `Pending`, so the pair is the unit:

```yaml
charts:
  - name: kube-vip
    source: https://github.com/kube-vip/helm-charts/releases/download/kube-vip-0.11.2/kube-vip-0.11.2.tgz
    shasum: cd7b5ab2...
    targetNamespace: kube-system
    images:
      - ghcr.io/kube-vip/kube-vip:v1.3.1
    values: |
      image:
        tag: v1.3.1
      env:
        svc_enable: "true"
        svc_election: "true"
        cp_enable: "false"
  - name: kube-vip-cloud-provider
    source: https://github.com/kube-vip/helm-charts/releases/download/kube-vip-cloud-provider-0.2.11/kube-vip-cloud-provider-0.2.11.tgz
    shasum: 44b0784f...
    targetNamespace: kube-system
    images:
      - docker.io/kubevip/kube-vip-cloud-provider:v0.0.13
    values: |
      cm:
        data:
          cidr-default: "{{ .Values.chart.addressPool }}"
```

Two things about it are worth copying. `image.tag` is pinned to the same kube-vip release the static pod flavor carries rather than left at the chart's `appVersion`, which lags - two packages in one repository disagreeing about which kube-vip they ship is a question nobody should have to answer. And the address pool is a *value*, not a default: a package cannot know which of a cluster's addresses are spare, so it ships a documentation-range placeholder that works nowhere and the cluster says.

### A control-plane VIP - the chart can do it, but not at bootstrap

The same chart holds the control-plane VIP, with `cp_enable` on and the address set:

```yaml
charts:
  - name: kube-vip
    source: https://github.com/kube-vip/helm-charts/releases/download/kube-vip-0.11.2/kube-vip-0.11.2.tgz
    shasum: cd7b5ab2...
    targetNamespace: kube-system
    images:
      - ghcr.io/kube-vip/kube-vip:v1.3.1
    values: |
      image:
        tag: v1.3.1
      config:
        address: "{{ .Cluster.LoadBalancer }}"
      env:
        cp_enable: "true"
        svc_enable: "false"
        vip_leaderelection: "true"
        lb_enable: "true"
        lb_port: "6443"
```

`{{ .Cluster.LoadBalancer }}` is the inventory's own `spec.config.loadbalancer` - see [package values](package-values.md#cluster-what-the-inventory-already-knows) - so the VIP is stated once for the cluster and the chart reads it.

**This cannot replace the static pod on a fresh install, and the reason is ordering rather than taste.** The chart installs a DaemonSet, and cargoship delivers a chart by writing a `HelmChart` resource for the engine's helm controller to act on - which needs an API server. But the VIP *is* how the second and third controllers reach the API server: cargoship writes `server: https://<loadbalancer>:<supervisor port>` on every controller after the first - 6443 on k3s, 9345 on rke2. So on a greenfield multi-controller cluster the chart cannot bring the address up in time, and controller two times out joining. A static pod has no such problem: the kubelet starts it from disk before the API server exists, which is why `example/k3s-kube-vip` and `example/rke2-kube-vip` use one.

So:

| Job                                   | Use                                   | Why                                                                                 |
| :------------------------------------ | :------------------------------------ | :---------------------------------------------------------------------------------- |
| Control-plane VIP, fresh install      | the static pod                        | it is up before the API server, which is when joining controllers need the address  |
| Control-plane VIP, cluster already up | either; the chart is the better shape | scoped RBAC, upgradeable with helm, no admin kubeconfig on disk in a privileged pod |
| Moving an existing VIP onto the chart | both, in that order                   | set `kubeVip.enabled: false` in the same apply, or two kube-vips hold one address   |
| Service load balancers                | the charts                            | nothing to bootstrap, and the cloud provider has no static-pod equivalent           |

What the static pod costs is worth being explicit about: it mounts the engine's admin kubeconfig - cluster-admin - into a host-network pod with `NET_ADMIN`, `NET_RAW` and `SYS_TIME`. The charts ship a ServiceAccount with a narrow ClusterRole instead. That is the argument for moving a control-plane VIP onto the chart once the cluster exists, and it is a real one; it just cannot be the thing that brings the cluster up.

Switching an already-installed cluster over is two changes in one apply, not one. Setting `chart.enabled: true` while `kubeVip.enabled` is still true leaves the static pod on disk and started by every kubelet, with the chart's DaemonSet configured to hold the same address - so set `kubeVip.enabled: false` at the same time, which drops the static pod from the package's desired files and has phase 50 prune it.

## Ordering

The helm controller applies charts in no guaranteed order, and a chart whose dependencies are not up yet retries rather than failing for good. A chart that needs the CNI, or a CRD another chart installs, will come up late rather than not at all - but nothing in the package can express "after that one".
