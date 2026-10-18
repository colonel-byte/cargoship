# Why the node phases use the dynamic client and a hand-written struct

Three places in cargoship read Kubernetes nodes, and none of them uses a typed client. From the code alone each one looks like the awkward way to do an easy thing - `unstructured.NestedBool` instead of `node.Spec.Unschedulable`, a hand-written struct instead of `corev1.NodeList`. This records why, because the obvious cleanup costs 24MB.

## Every typed client registers every API group

`k8s.io/client-go/kubernetes/typed/core/v1` imports `k8s.io/client-go/kubernetes/scheme`, and that package's `init` registers **every** Kubernetes API group - admissionregistration, flowcontrol, resource, all of them. The import is not optional and not per-group: taking the typed `core/v1` client takes the scheme, and the scheme takes the rest.

This was measured rather than assumed. Narrowing `kubernetes.NewForConfig` to `corev1client.NewForConfig` - the typed client for the one group cargoship touches - moved the binary by **0.01MB** and left 56 API groups linked. The typed clients are all one dependency, whichever one you pick.

What worked was avoiding the scheme:

| Client                                   | `k8s.io/api` groups linked |
| ---------------------------------------- | -------------------------- |
| `client-go/kubernetes` (typed clientset) | 59                         |
| `client-go/kubernetes/typed/core/v1`     | 56                         |
| `client-go/dynamic`                      | **0**                      |
| `client-go/tools/clientcmd`              | **0**                      |

`k8s.io/api` 14MB plus `client-go` 9.4MB, against five call sites that read a node's name, its labels, `spec.unschedulable` and the Ready condition.

## What each place does instead

**`pkg/phase/81_label_nodes.go`** uses `dynamic.Interface` against `schema.GroupVersionResource{Version: "v1", Resource: "nodes"}`. It lists nodes and patches one label, and `unstructured.Unstructured` serves both directly: `GetName` and `GetLabels` are methods on it, and `Patch` takes the same merge-patch bytes the typed client took. The phase reads no field that needs a typed struct.

**`pkg/phase/13_detect_removed_hosts.go`** does not use a client at all. It shells out to `kubectl get nodes -o json` on the leader over SSH - deliberately, so the check works against a cluster whose firewall admits only SSH - and used `corev1.NodeList` purely as a JSON decoding target. `kubectlNodeList` and `kubectlNode` are that shape in twelve lines. `kubectlNodeAddress` carries only the address, because the comparison matches any address a node reports against any identifier a host carries and never looks at the type discriminator.

**`test/kube.go`** and the cluster suite read the Ready condition and `spec.unschedulable` through `unstructured.NestedSlice` and `NestedBool`. A missing or malformed field reads as not-ready and not-cordoned, which are the right answers while a node is still coming up. `NodesResource` is exported so the suite and the phase do not disagree about the GVR.

## Do not replace these with the typed client

The typed version of any of this is shorter and reads better. It also relinks 24MB for four fields. If a future phase genuinely needs a typed Kubernetes object - a full `PodSpec`, something with defaulting, anything where the field set matters - that is the moment to reconsider, and the right argument then is that the object is too complex to spell by hand, not that `GetLabels` is ugly.

`clientcmd` is unaffected and still typed: it parses and writes kubeconfigs, pulls no API group, and `pkg/phase/80_kubeconfig.go` and `types/distrocfg/admin_credentials.go` use it as they always did.
