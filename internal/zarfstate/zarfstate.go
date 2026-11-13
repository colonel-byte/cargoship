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
	"net"
	"net/url"

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

	// serverTLSSecretName holds the registry's server certificate and the CA that signed it,
	// and caKey is the CA within it. Zarf mounts the same secret into the registry pod as its
	// `server-certs` volume, so this is the certificate chain a push has to validate against
	// rather than a copy of it.
	//
	// The CA is not in zarf state: RegistryInfo records the address, the port, the mode and
	// the mTLS strategy and nothing else, so a client that has to trust the registry has to
	// come here for it.
	serverTLSSecretName = "zarf-registry-server-tls"
	caKey               = "ca.crt"
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

// Options say which cluster to read, and how to reach it.
type Options struct {
	// Kubeconfig is the file to read. Empty takes the ambient configuration -- KUBECONFIG, else
	// the standard location.
	Kubeconfig string
	// Context is the context within it. Empty takes whichever one it marks current.
	Context string
	// Dial, when set, carries the connection to the API server, which is how the state of a
	// cluster whose API this machine cannot route to is read -- see pkg/tunnel.
	//
	// The kubeconfig's server URL is deliberately left alone when this is set. TLS still
	// validates against the hostname it names, using the CA it names, so the credentials keep
	// working and no certificate SAN has to cover the address actually dialled. The kubeconfig
	// supplies the identity; Dial supplies the route.
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
}

// Load reads the registry half of the zarf-state secret.
func Load(ctx context.Context, opts Options) (Registry, error) {
	restConfig, err := RESTConfig(opts.Kubeconfig, opts.Context)
	if err != nil {
		return Registry{}, err
	}
	restConfig.Dial = opts.Dial

	raw, err := readSecretKey(ctx, restConfig, secretName, dataKey)
	if err != nil {
		return Registry{}, fmt.Errorf("unable to read the registry a zarf deployment records: %w", err)
	}

	return decode(raw)
}

// LoadCABundle reads the CA that signed the registry's server certificate.
//
// A zarf-managed-mTLS registry serves HTTPS with a certificate from a private CA, so a push to
// it validates against nothing the system roots hold. This is the one piece of that puzzle not
// in zarf state, and reading it out of the cluster is what lets a mirror verify the registry
// properly rather than being told to skip verification.
//
// No client certificate is read, and that is deliberate rather than an omission. Zarf issues one
// -- the `zarf-registry-client-tls` secret, common name `zarf-registry-client` -- but it belongs
// to the per-node registry proxy, which is what presents it on a node's behalf. The registry
// itself does not require one from a direct client, so a push authenticates with the push
// credentials from state and nothing more.
func LoadCABundle(ctx context.Context, opts Options) ([]byte, error) {
	restConfig, err := RESTConfig(opts.Kubeconfig, opts.Context)
	if err != nil {
		return nil, err
	}
	restConfig.Dial = opts.Dial

	ca, err := readSecretKey(ctx, restConfig, serverTLSSecretName, caKey)
	if err != nil {
		return nil, fmt.Errorf("unable to read the registry's certificate authority: %w", err)
	}
	return ca, nil
}

// readSecretKey reads one key out of one secret in zarf's namespace.
//
// It goes through the dynamic client for the reason secretsResource records, and it is shared so
// that the state read and the CA read cannot disagree about the namespace or about how a missing
// key is reported.
func readSecretKey(ctx context.Context, restConfig *rest.Config, name, key string) ([]byte, error) {
	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to build a kubernetes client: %w", err)
	}

	secret, err := client.Resource(secretsResource).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("unable to read the %s/%s secret: %w", namespace, name, err)
	}

	return secretKey(secret.Object, name, key)
}

// secretKey pulls one base64 value out of an unstructured secret. Separate from the read so that
// the shape handling is testable without a cluster.
func secretKey(obj map[string]any, name, key string) ([]byte, error) {
	data, ok := obj["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("the %s/%s secret holds no data", namespace, name)
	}
	encoded, ok := data[key].(string)
	if !ok {
		return nil, fmt.Errorf("the %s/%s secret has no %q key", namespace, name, key)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("unable to decode %q in the %s/%s secret: %w", key, namespace, name, err)
	}
	return raw, nil
}

// APIEndpoint is the address a tunnel to the API server should open onto, derived from the
// kubeconfig's own server URL: the loopback address, and the port that URL names.
//
// The port is read rather than assumed because it is the one piece of this that varies -- k3s
// and RKE2 both serve 6443, but a cluster behind a load balancer on another port records that,
// and guessing would tunnel to a closed port on the node. The host is loopback because the API
// server listens there on every control-plane node, which is what makes any controller do.
func APIEndpoint(kubeconfig, kubeContext string) (string, error) {
	restConfig, err := RESTConfig(kubeconfig, kubeContext)
	if err != nil {
		return "", err
	}

	u, err := url.Parse(restConfig.Host)
	if err != nil {
		return "", fmt.Errorf("unable to read the kubeconfig's server address %q: %w", restConfig.Host, err)
	}

	port := u.Port()
	if port == "" {
		// A URL with no port means the scheme's default, and a kubeconfig's is always https.
		port = "443"
	}
	return net.JoinHostPort("127.0.0.1", port), nil
}

// RESTConfig resolves a kubeconfig path and a context name into a connection.
//
// This goes through the deferred loading rules rather than clientcmd.BuildConfigFromFlags
// because that helper has no way to name a context -- it always takes whichever one the file
// marks current, which is the wrong cluster as soon as an operator has more than one.
//
// It is exported because a caller reaching the cluster for something other than a secret has to
// resolve the same kubeconfig and the same context this package did, rather than loading its own
// and quietly disagreeing about which cluster is meant.
func RESTConfig(kubeconfig, kubeContext string) (*rest.Config, error) {
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
