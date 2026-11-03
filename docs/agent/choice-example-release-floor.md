# Why the examples stop at N-2

Every example under `example/` is rendered for a set of Kubernetes minor lines, and that set now has a floor: `exampleMinorFloor` in [`magefiles/pkg/gen/examples/tags.go`](../../magefiles/pkg/gen/examples/tags.go), currently `v1_35`. The newest line and the two before it are rendered; anything older is not, for rke2, k3s, and upstream alike.

Before this, the floor was wherever `thirdparty-src/pins.json` happened to still pin, plus whatever was already committed -- `exampleTags` adds every version directory it finds on disk to the set it renders, so a line stayed alive by having been rendered once. That is how `v1_32` was still being re-rendered on every refresh, long after anybody would install it.

## Why a floor at all, rather than letting the pins decide

An example is not just a version string. It is a set of components that have to work together: the distro build, the kubeadm-published OS packages, containerd, and -- for the upstream examples -- a CNI the package applies itself.

The CNI is what forces the question. A CNI release supports a handful of Kubernetes minors and says which ones: cilium e2e tests exactly four and states outright that "older Kubernetes versions not listed here do not have Cilium support" (`Documentation/network/kubernetes/requirements.rst`, in the cilium tree). Cilium 1.20 covers 1.33 through 1.36. So an `upstream-cilium` example rendered for 1.32 would carry a cilium that cilium does not claim works there -- a package that builds, publishes, and installs, and then does not route pod traffic.

Pinning a CNI per minor line would answer that differently, and it was considered. It means a version map per CNI, a rendered cilium manifest per chart version, and a judgement call per line about which CNI release to trust -- all to keep alive lines that Kubernetes itself no longer supports. N-2 matches what upstream Kubernetes supports, which is the same window the CNIs are tested against, so one floor covers both and there is no map to maintain.

## Why moving it is two hand edits

The floor is a constant, and tags below it are dropped rather than rendered. What is already committed below the floor is not deleted by a render.

A generator that deleted a whole example tree because a pin moved would be doing something a reader of `mage generate:examples` has no reason to expect, and the thing it deleted is the one copy of a definition somebody may be installing from. Retired rke2 builds are pruned automatically ([`writeExample`](../../magefiles/pkg/gen/examples/examples.go)) and that is a different case: an `rke2rN` whose RPMs Rancher has already removed cannot be installed from at all, so keeping it only invites someone to try. A 1.34 example still installs fine. It is just outside what this repository carries.

So raising the floor is: edit the constant, and `git rm` the lines that fall below it. Anything missed shows up as a tree that stops changing, not as a tree that silently keeps claiming to be current.

## What this cost

Three minor lines per flavor, deleted: `example/*/v1_32`, `v1_33`, `v1_34`, along with their `thirdparty-src/<distro>/` source copies and their generated `pkg/engineconfig/gen/<distro>/` packages. That is a breaking change for anyone pointing at one of those paths, and it is recoverable from git history rather than regenerable -- the upstream artifacts those examples name are still published, so a checkout of the commit before this one renders them again.
