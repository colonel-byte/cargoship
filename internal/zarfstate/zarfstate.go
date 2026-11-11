// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package zarfstate reads the registry half of a Zarf deployment's state secret, so that a
// mirror can be pointed at a registry Zarf already stands up and authenticate against it with
// the credentials Zarf already generated.
package zarfstate

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Where Zarf keeps its state. These mirror state.ZarfNamespaceName, ZarfStateSecretName and
// ZarfStateDataKey in vendor/github.com/zarf-dev/zarf/src/pkg/state, which are not imported --
// see the note on Registry.
const (
	namespace  = "zarf"
	secretName = "zarf-state"
	dataKey    = "state"
)

// Registry modes, matching state.RegistryMode. Only an external registry records an address
// that is reachable from somewhere other than a cluster node.
const (
	ModeNodePort = "nodeport"
	ModeProxy    = "proxy"
	ModeExternal = "external"
)

// secretsResource names the resource Load reads.
//
// Reading it goes through the dynamic client rather than the typed clientset for the reason
// pkg/phase/81_label_nodes.go records: every typed client imports
// k8s.io/client-go/kubernetes/scheme, which registers every Kubernetes API group and links
// about 24MB into the binary. This reads one key out of one secret, which unstructured serves
// directly.
var secretsResource = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

// Registry is the part of Zarf's state a mirror needs: where the registry is, and how to
// authenticate a push to it.
//
// The fields are declared here rather than taken from zarf's state.RegistryInfo because that
// package pulls src/api/convert, src/api/v1beta1, src/pkg/pki and src/pkg/utils in behind it,
// none of which is otherwise linked -- the transitive-import trap docs/agent/choice-coci-fork.md
// records. The json tags are the contract, and they are what has to keep matching.
//
// shortcut: hand-rolled struct instead of zarf's state.RegistryInfo; switch to the import if
// zarf's state JSON shape changes or that package becomes linked for another reason.
type Registry struct {
	Address      string `json:"address"`
	Port         int    `json:"port"`
	NodePort     int    `json:"nodePort"`
	PushUsername string `json:"pushUsername"`
	PushPassword string `json:"pushPassword"`
	Mode         string `json:"registryMode"`
	MTLSStrategy string `json:"mtlsStrategy"`
}

// IsExternal reports whether the registry runs outside the cluster, which is the one case where
// the recorded address is reachable from a management node. An internal registry is reached
// through a NodePort or a per-node host port, and Zarf itself tunnels to it.
//
// An empty mode is how Zarf wrote state before it recorded one; it meant external whenever an
// address was set, which is the same rule state.RegistryInfo.FillInEmptyValues applies.
func (r Registry) IsExternal() bool {
	if r.Mode == "" {
		return r.Address != ""
	}
	return r.Mode == ModeExternal
}

// UsesMTLS reports whether Zarf manages mTLS certificates for the registry, in which case a
// push needs a client certificate this package does not read.
func (r Registry) UsesMTLS() bool {
	return r.MTLSStrategy != "" && r.MTLSStrategy != "none"
}

// Load reads the registry half of the zarf-state secret through the given kubeconfig and
// context. An empty kubeconfig takes the ambient configuration -- KUBECONFIG, else the standard
// location -- and an empty kubeContext takes whichever context that configuration marks current.
func Load(ctx context.Context, kubeconfig, kubeContext string) (Registry, error) {
	restConfig, err := restConfigFor(kubeconfig, kubeContext)
	if err != nil {
		return Registry{}, err
	}

	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return Registry{}, fmt.Errorf("unable to build a kubernetes client: %w", err)
	}

	secret, err := client.Resource(secretsResource).Namespace(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return Registry{}, fmt.Errorf("unable to read the %s/%s secret, which is where a zarf deployment records its registry: %w", namespace, secretName, err)
	}

	data, ok := secret.Object["data"].(map[string]any)
	if !ok {
		return Registry{}, fmt.Errorf("the %s/%s secret holds no data", namespace, secretName)
	}
	encoded, ok := data[dataKey].(string)
	if !ok {
		return Registry{}, fmt.Errorf("the %s/%s secret has no %q key", namespace, secretName, dataKey)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Registry{}, fmt.Errorf("unable to decode the %s/%s secret: %w", namespace, secretName, err)
	}

	return decode(raw)
}

// restConfigFor resolves a kubeconfig path and a context name into a connection.
//
// This goes through the deferred loading rules rather than clientcmd.BuildConfigFromFlags
// because that helper has no way to name a context -- it always takes whichever one the file
// marks current, which is the wrong cluster as soon as an operator has more than one.
func restConfigFor(kubeconfig, kubeContext string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}

	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules,
		&clientcmd.ConfigOverrides{CurrentContext: kubeContext},
	).ClientConfig()
	if err != nil {
		if kubeContext != "" {
			return nil, fmt.Errorf("unable to use the kubeconfig context %q: %w", kubeContext, err)
		}
		return nil, fmt.Errorf("unable to read the kubeconfig: %w", err)
	}
	return restConfig, nil
}

// decode reads the registry out of the state JSON. It is separate from Load because this is
// where every decision about the shape of zarf's state lives, and it is testable without a
// cluster.
func decode(stateJSON []byte) (Registry, error) {
	var state struct {
		RegistryInfo Registry `json:"registryInfo"`
	}
	if err := json.Unmarshal(stateJSON, &state); err != nil {
		return Registry{}, fmt.Errorf("unable to read the zarf state: %w", err)
	}

	reg := state.RegistryInfo
	// Older zarf wrote the port as nodePort only, and state written then is still readable.
	if reg.Port == 0 {
		reg.Port = reg.NodePort
	}
	if reg.PushUsername == "" || reg.PushPassword == "" {
		return Registry{}, errors.New("the zarf state records no registry push credentials")
	}
	return reg, nil
}
