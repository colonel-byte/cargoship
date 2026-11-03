# Why `pkg/packager/actions` is a fork, and why a create-time cluster wait now fails

cargoship runs the `onCreate` actions in a distro package through zarf's action executor. `runCreateActions` in [`pkg/packager/assemble/actions.go`](../golang/pkg/packager/assemble.md) renders each action with cargoship's template engine and then hands it to `actions.Run`, which owns everything that makes an action an action: shells, retries, timeouts, output capture, `setVariables` and `setValues`. Only the rendering was ever cargoship's - see [`choice-v1alpha1-action-conversion.md`](choice-v1alpha1-action-conversion.md) for why the conversion into zarf's normalized types happens at the call site.

The executor is now a fork. The reason is one import.

## What the import cost

zarf's `src/pkg/packager/actions` imports `src/pkg/wait`, and uses it in exactly one function, `runWaitAction`. zarf's `wait` package serves both kinds of wait from one file: `ForNetwork`, which probes HTTP or TCP with the standard library, and `ForResource`, which is `kubectl wait`'s logic with retries for a resource that does not exist yet. The second one brings:

```
zarf/src/pkg/wait
  -> k8s.io/kubectl/pkg/cmd/wait + cmd/get
    -> k8s.io/cli-runtime/pkg/resource
      -> sigs.k8s.io/kustomize/api + kyaml
  -> zarf/src/pkg/cluster
```

Measured: **92.98MB to 87.11MB, 5.87MB**, and `k8s.io/kubectl`, `sigs.k8s.io/kustomize`, `k8s.io/cli-runtime`, `zarf/src/pkg/wait` and `zarf/src/pkg/cluster` all go to zero in `go list -deps ./cmd/cargoship`. Go cannot drop a transitive import, so the only way to stop linking kubectl is to stop linking the package that imports the package that imports it.

## Why a cluster wait was droppable

`Actions` in the distro spec has one field, `OnCreate`. There is no deploy-time or remove-time action path in cargoship: actions run while the package is being built, and nothing else runs them. A package is built on a management node that is typically air-gapped.

So `wait.cluster` in an `onCreate` action asks cargoship to wait for a Kubernetes resource at a point where there is no cluster in the picture. Upstream would resolve a client from the ambient kubeconfig, which is either absent - and the action fails five minutes later with a connection error - or present and pointing at some cluster that is not the one this package will be applied to. Neither is a thing an operator wants.

`runWaitClusterAction` therefore fails immediately, naming the action and the resource, and says to use `wait.network` instead. `wait.network` is unaffected: the network half of zarf's `wait.go` is forked into `pkg/packager/actions/wait.go`, and it is standard-library HTTP and TCP probing with `retry-go` around it.

## This is a behaviour change, not dead code removal

`wait` reaches `schema/distro-v1alpha1-schema.json` because `Actions.OnCreate` embeds `zarf.ZarfComponentActionSet`, so a `wait.cluster` action is something the published schema accepts and an operator could have written. Nothing in the repo uses one - no example, no fixture, no test - and the e2e action fixture exercises `cmd` actions only. But absence from this repo is not absence from every `distro.yaml`.

The schema was deliberately left alone. Narrowing it would mean retyping `Actions.OnCreate` off zarf's action set, which is the schema break `choice-v1alpha1-action-conversion.md` exists to avoid, and it would reject the field at parse time instead of explaining the problem at the point of use. A package carrying a create-time cluster wait now fails with a message that says what to do; that is better than the wait it used to get, and better than a schema validation error that cannot explain itself.

## The other upstream divergence

`Run` used to build a `VariableConfig` when handed nil, out of `src/internal/packager/template` - an internal package a fork cannot import. Every caller here supplies one and has to: a config built per call loses the variables an earlier action set, so `setVariables` would not reach the action after it, which is the bug `newVariableConfig` was written to avoid. The fork returns an error for a nil config rather than quietly running with one that forgets.

## Resyncing with upstream

Three places diverge, and nothing else should. When pulling zarf's `actions.go` forward, re-apply exactly these:

1. the `src/pkg/wait` import is dropped,
2. `runWaitClusterAction` returns the refusal rather than calling `wait.ForResource`,
3. `runWaitNetworkAction` calls this package's `ForNetwork`, and `Run` errors on a nil `VariableConfig` instead of calling `ptmpl.GetZarfVariableConfig`.

Everything else in the file is zarf's and should stay byte-for-byte theirs, because the action semantics an operator relies on - retry counts, timeout handling, how `mute` interacts with output capture - are upstream's to define.
