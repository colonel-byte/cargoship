# Why the upstream examples carry a CNI, and how each one gets its manifest

Upstream (kubeadm) installs no CNI. A cluster built from an upstream example without one forms, passes every check cargoship makes, and leaves every pod at `ContainerCreating` forever. `spec.config.manifests` exists for exactly this: cargoship kubectl-applies what a package names there from the leader once it has bootstrapped ([`pkg/phase/64_apply_manifests.go`](../../pkg/phase/64_apply_manifests.go)), and only the upstream distro implements `ManifestPaths`, because rke2 and k3s ship their CNI with the release and configure it through a `HelmChartConfig` instead.

So the question is not whether an upstream example declares a CNI. It is which one, and where the manifest comes from -- and the three CNIs answer that differently enough that each gets its own flavor directory rather than one example with a knob.

## One flavor per CNI

`example/upstream-cilium`, `example/upstream-canal`, `example/upstream-flannel`. The CNI decides more than the image list:

- the manifest that gets applied,
- the pod CIDR kubeadm has to be told about, since flannel and canal hardcode `10.244.0.0/16` in the manifest they publish and cilium reads the cluster's,
- whether kube-proxy stays.

A single example with a values-driven CNI could not express the second: the pod CIDR goes into `kubeadm init`'s `ClusterConfiguration`, which is read once at bootstrap, so it has to be in the definition rather than chosen later. The rke2 examples already split per CNI for the same reason.

## flannel and canal: a pinned URL

Both publish a manifest per release. The flavor pins the URL with its version in it, cargoship verifies the download against the digest in `example/shasums.json`, and the generator reads the same file to pull out the images.

The URL names a version rather than using `releases/latest/download`. An alias serves different bytes from one stable URL, and `example/shasums.json` keys entries by file name -- so `kube-flannel.yml` would flip digests on every upstream release, which is precisely the "same name, different content" case the cache treats as a cache miss worth re-hashing.

## Cilium: rendered, because there is nothing to download

Cilium stopped publishing an applyable manifest around v1.10; `install/kubernetes/quick-install.yaml` 404s. What exists is the Helm chart. Cargoship has no Helm dependency and the apply phase takes `kubectl apply -f` paths, so the manifest is produced at generate time with `helm template` and committed.

**Rendered once per chart version, not once per example.** It lands in `example/upstream-cilium/cni/cilium-<chart>.yaml` and every version directory refers to it as `../../cni/…`, the way k3s examples share `example/k3s/core/`. Nineteen committed copies of a 65 KB manifest would otherwise say the same thing nineteen times.

**The render is skipped while the file exists.** This is what keeps `helm` out of the loop that keeps the examples current: only a chart pin bump produces a path that is not already in the repository, so `mage generate:examples` on a committed tree never shells out to helm, and `refresh-examples.yaml` -- which runs weekly and never touches the pin -- needs nothing but the Go toolchain. The cost of a new pinned tool falls on whoever moves the pin, locally.

### `hubble.tls.auto.method=cronJob` is not optional

The chart's default (`helm`) generates Hubble's CA keypair *while templating* and writes it into a Secret in the output -- it labels that object `cilium.io/helm-template-non-idempotent` itself. Two consequences, both disqualifying:

- the render stops being reproducible, so every regeneration produces a diff,
- the private key would be committed, handing every cluster installed from the example the same Hubble CA.

`cronJob` has cilium's own certgen job mint the certificates in the cluster instead. The manifest then carries no key and renders byte-identically every time; the certgen image it adds is picked up by the image scrape like any other.

`refuseEmbeddedKey` fails the render if a key shows up anyway. A chart's defaults can change under a version bump, and this failure is silent by nature -- a committed key looks like any other base64 blob in a diff of a file marked `linguist-generated`.

## Images come out of the manifest

Each flavor's `imageConfig.images` is the kubeadm-pinned control plane list plus whatever `image:` lines the manifest it applies carries. A second list maintained by hand would agree with the manifest until the CNI added a sidecar, and the failure shows up only on an air-gapped host at apply time -- the one place it is most expensive to find.

## What was not done

**Kube-proxy-free cilium.** `upstream-cilium` runs cilium alongside kube-proxy, unlike `example/rke2-multi-cni-cilium` which replaces it. Replacing it needs `InitConfiguration.skipPhases: [addon/kube-proxy]`, which [`types/distrocfg/upstream_kubeadm.go`](../../types/distrocfg/upstream_kubeadm.go) does not render, plus a `k8sServiceHost` that only runtime cluster state knows. That is a feature in the kubeadm config path, not an examples change.

**Helm chart support in the apply phase.** It would let the package ship the chart instead of a rendered manifest, and would be the more faithful way to install cilium. It is also a new surface in `api/` and `pkg/phase/` for a problem `helm template` solves at generate time.
