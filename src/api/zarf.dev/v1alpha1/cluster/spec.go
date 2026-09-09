// Copyright 2023, k0sctl authors.
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
//
// Modifications Copyright 2026 colonel-byte.
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

// Package cluster defines the API types for a cluster configuration.
package cluster

import (
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/src/types"
	"github.com/invopop/jsonschema"
)

// ZarfCluster is the root object of a cluster configuration document.
type ZarfCluster struct {
	// APIVersion identifies the API group and version of this configuration document.
	APIVersion string `json:"apiVersion,omitempty" jsonschema:"enum=zarf.dev/v1alpha1"`
	// Kind identifies the document type. The value must be ZarfCluster.
	Kind v1alpha1.ZarfDistroKind `json:"kind" jsonschema:"enum=ZarfCluster"`
	// Metadata holds identifying information for the cluster.
	Metadata ZarfClusterMetadata `json:"metadata"`
	// Spec holds the configuration and hosts for the cluster.
	Spec ZarfClusterSpec `json:"spec"`
	// RuntimeMetadata stores data gathered while the phases run.
	RuntimeMetadata ZarfRuntimeMeta `json:"-"`
}

// ZarfClusterMetadata holds identifying information for a cluster.
type ZarfClusterMetadata struct {
	// Name sets the cluster name. If you allow cargoship to update the kubeconfig, cargoship uses this name there.
	Name string `json:"name" jsonschema:"pattern=^[a-z0-9][a-z0-9\\-]*$"`
}

// ZarfRuntimeMeta stores data gathered while the phases run.
type ZarfRuntimeMeta struct {
	// ControllerTLS lists the names and addresses on the controller TLS certificate.
	ControllerTLS []string
	// ControllerToken authorizes a worker node to join the cluster as a controller.
	ControllerToken string
	// AgentToken authorizes a worker node to join the cluster as an agent.
	AgentToken string
	// LoadBalancer is the hostname clients use to reach the cluster control plane.
	LoadBalancer string
	// Leader is the controller host that stores the cluster join tokens.
	Leader *ZarfHost
	// Registries lists the container registries the cluster uses.
	Registries []ZarfClusterRegistries
}

// ZarfClusterSpec holds the configuration and hosts for a cluster.
type ZarfClusterSpec struct {
	// Config holds the cluster-wide configuration.
	Config ZarfClusterConfig `json:"config"`
	// Hosts lists the hosts that make up the cluster.
	Hosts ZarfHosts `json:"hosts" jsonschema:"minItems=1"`
}

// ZarfClusterConfig holds cluster-wide configuration.
type ZarfClusterConfig struct {
	// LoadBalancer is the hostname clients use to reach the cluster control plane.
	LoadBalancer string `json:"loadbalancer" jsonschema:"format=hostname"`
	// Registries lists the container registries the cluster uses.
	Registries []ZarfClusterRegistries `json:"registries,omitempty"`
	// Profiles maps a profile name to host and engine overrides that a host can select.
	Profiles map[string]ZarfClusterProfiles `json:"profiles,omitempty"`
}

// ZarfClusterProfiles holds the host and engine overrides for one profile.
type ZarfClusterProfiles struct {
	// Host holds the configuration overrides applied to a host that selects this profile.
	Host ZarfHostConfig `json:"host,omitempty"`
	// Engine holds the node label and taint overrides applied to a host that selects this profile.
	Engine ZarfHostEngine `json:"engine,omitempty"`
	// Concurrency limits how many hosts using this profile cargoship processes at once, e.g.
	// draining, upgrading, initializing, or uninstalling. It accepts a fixed count ("1") or a
	// percentage of the hosts sharing this profile ("25%"). Empty falls back to the phase's
	// default concurrency.
	Concurrency string `json:"concurrency,omitempty" jsonschema:"oneof_type=string;integer" jsonschema_extras:"examples=1,examples=5,examples=25%,examples=100%"`
}

// ResolveConcurrency returns the batch size cargoship should use for total hosts sharing this
// profile. A fixed count is used as-is. A percentage (e.g. "25%") is scaled against total,
// rounded up, and clamped to a minimum of 1 so a low percentage never collapses to 0, which
// would otherwise be indistinguishable from "unlimited". An empty Concurrency falls back to
// fallback, parsed the same way against total.
func (p ZarfClusterProfiles) ResolveConcurrency(total int, fallback string) (int, error) {
	c := strings.TrimSpace(p.Concurrency)
	if c == "" {
		c = fallback
	}
	return ParseConcurrency(c, total)
}

// ParseConcurrency parses a concurrency spec (a fixed count like "1", or a percentage like
// "25%") against total, the number of hosts in the batch being sized. A fixed count is used
// as-is; 0 or empty means unlimited. A percentage is scaled against total, rounded up, and
// clamped to a minimum of 1 so a low percentage never collapses to 0, which would otherwise be
// indistinguishable from "unlimited".
func ParseConcurrency(value string, total int) (int, error) {
	c := strings.TrimSpace(value)
	if c == "" {
		return 0, nil
	}

	if pct, ok := strings.CutSuffix(c, "%"); ok {
		v, err := strconv.Atoi(strings.TrimSpace(pct))
		if err != nil {
			return 0, fmt.Errorf("invalid concurrency percentage %q: %w", value, err)
		}
		if v < 0 || v > 100 {
			return 0, fmt.Errorf("invalid concurrency percentage %q: must be between 0 and 100", value)
		}
		scaled := (total*v + 99) / 100
		if scaled < 1 {
			scaled = 1
		}
		return scaled, nil
	}

	v, err := strconv.Atoi(c)
	if err != nil {
		return 0, fmt.Errorf("invalid concurrency %q: %w", value, err)
	}
	if v < 0 {
		return 0, fmt.Errorf("invalid concurrency %q: must not be negative", value)
	}
	return v, nil
}

// ZarfClusterRegistrieName is the registry name in ZarfClusterRegistries. It's a named
// type (rather than a bare string) solely so it can implement JSONSchemaExtend below and
// suggest common registries in the generated schema; the config file is not restricted to those.
type ZarfClusterRegistrieName string

// JSONSchemaExtend adds CommonRegistries to the schema as examples, so editors can
// suggest them for this string field without restricting it to just those values.
func (ZarfClusterRegistrieName) JSONSchemaExtend(s *jsonschema.Schema) {
	for _, registry := range types.CommonRegistries {
		s.Examples = append(s.Examples, registry)
	}
}

// ZarfClusterRegistries holds the credentials and pull proxy for one container registry. The
// proxy and the credentials are independent: an entry with both redirects pulls to a mirror and
// authenticates to that mirror, while an entry with credentials alone authenticates a direct
// pull from the registry itself. Upstream Kubernetes keeps the two in separate files entirely --
// mirrors in containerd's hosts.toml, which holds no credentials, and credentials in a node
// docker config -- so an entry must be able to carry either one on its own.
type ZarfClusterRegistries struct {
	// Name identifies the registry. With a proxy it is the registry pulls are redirected away
	// from; without one it is the registry the credentials authenticate to.
	Name ZarfClusterRegistrieName `json:"name"`
	// Authentication holds the credentials for the registry.
	Authentication ZarfClusterRegistryAuth `json:"auth,omitempty"`
	// Proxy holds the pull redirect settings for the registry. Omit it to configure credentials
	// or TLS for the registry without redirecting pulls away from it.
	Proxy *ZarfClusterRegistryProxy `json:"proxy,omitempty"`
	// TLS configures TLS verification and certificates for the registry endpoint.
	TLS *ZarfClusterRegistryTLS `json:"tls,omitempty"`
}

// Validate returns an error when the registry entry configures nothing. An entry has to carry a
// proxy, credentials, or TLS settings to have any effect, and one that carries none of them is
// more likely a mistyped document than an intentional no-op.
func (r ZarfClusterRegistries) Validate() error {
	if err := validateRegistryName(r.Name); err != nil {
		return err
	}
	if err := r.TLS.Validate(r.Name); err != nil {
		return err
	}
	if err := r.Proxy.Validate(r.Name); err != nil {
		return err
	}
	if err := r.Authentication.Validate(r.Name); err != nil {
		return err
	}
	if r.ProxyURL() != "" || r.TLS != nil {
		return nil
	}
	if r.Authentication.Username != "" || r.Authentication.Password != "" || r.Authentication.Token != "" {
		return nil
	}
	return fmt.Errorf("registry %q: needs at least one of proxy.url, auth, or tls", r.Name)
}

// ProxyURL returns the address pulls for this registry are redirected to, or an empty string
// when the registry has no proxy and is pulled from directly. Proxy is a pointer -- so that a
// proxy left out of a document is distinguishable from one written out empty, and so that
// omitempty applies to it at all, which encoding/json does not do for a struct value -- and this
// saves every caller a nil check to ask the only question most of them have.
func (r ZarfClusterRegistries) ProxyURL() string {
	if r.Proxy == nil {
		return ""
	}
	return r.Proxy.URL
}

// MirrorEndpoint returns the proxy address as an engine mirror endpoint: the configured URL,
// completed to https when it was given without a scheme, since that is both what a registry
// serves by default and what the engine would otherwise have to guess at. It is empty when the
// registry has no proxy.
func (r ZarfClusterRegistries) MirrorEndpoint() string {
	proxy := r.ProxyURL()
	switch {
	case proxy == "":
		return ""
	case endpointSchemeRegex.MatchString(proxy):
		return proxy
	default:
		return "https://" + proxy
	}
}

// ConfigHost returns the host this registry's credentials and TLS settings belong to: the mirror
// when pulls are redirected to one, and the registry itself when they are not. The proxy address
// may carry a scheme and a path -- "https://mirror.example.com:5000/v2" -- but an engine matches
// its per-registry configuration on the host alone, so the host is what comes back here.
func (r ZarfClusterRegistries) ConfigHost() string {
	endpoint := r.MirrorEndpoint()
	if endpoint == "" {
		return string(r.Name)
	}
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	host, _, _ := strings.Cut(endpoint, "/")
	return host
}

// sameRegistryConfig reports whether two registry entries would write identical credentials and
// TLS settings, in which case both landing on the same host is harmless rather than a conflict.
func sameRegistryConfig(a, b ZarfClusterRegistries) bool {
	if a.Authentication != b.Authentication {
		return false
	}
	switch {
	case a.TLS == nil && b.TLS == nil:
		return true
	case a.TLS == nil || b.TLS == nil:
		return false
	default:
		return *a.TLS == *b.TLS
	}
}

// ValidateRegistries validates every registry entry, and rejects a set of entries that would
// quietly lose part of itself on the way to an engine configuration file.
//
// Two entries naming the same registry collide on the mirror they define for it, and two entries
// resolving to the same host -- which several registries proxied through one mirror legitimately
// do -- collide on the credentials and TLS settings written for that host. In both cases the
// last entry wins and the earlier one silently does nothing, so the conflict is reported here
// instead. Entries that resolve to the same host with identical settings are left alone: there
// is nothing to lose between them.
func ValidateRegistries(registries []ZarfClusterRegistries) error {
	byName := map[ZarfClusterRegistrieName]struct{}{}
	byHost := map[string]ZarfClusterRegistries{}
	for _, registry := range registries {
		if err := registry.Validate(); err != nil {
			return err
		}
		if _, ok := byName[registry.Name]; ok {
			return fmt.Errorf("registry %q: listed more than once", registry.Name)
		}
		byName[registry.Name] = struct{}{}

		host := registry.ConfigHost()
		previous, ok := byHost[host]
		if ok && !sameRegistryConfig(previous, registry) {
			return fmt.Errorf("registries %q and %q both configure host %q with different auth or tls settings", previous.Name, registry.Name, host)
		}
		if !ok {
			byHost[host] = registry
		}
	}
	return nil
}

// ZarfClusterRegistryTLS holds the TLS connection settings for a container registry. The field
// names are cargoship's own, in the camelCase the rest of this file uses; distrocfg maps them
// onto whatever spelling each engine's registry configuration expects.
type ZarfClusterRegistryTLS struct {
	// CA is the PEM-encoded CA certificate used to verify the registry certificate. Cargoship
	// writes it to a file on each host and points the engine at that path, so the certificate
	// travels with the cluster configuration instead of having to be distributed separately.
	// Use CAFile instead when the certificate is already on the hosts. It may also be given as
	// an Ansible Vault-encrypted string, which cargoship decrypts at apply time.
	CA string `json:"ca,omitempty"`
	// CAFile is the path on the host to the CA bundle used to verify the registry certificate.
	CAFile string `json:"caFile,omitempty"`
	// CertFile is the path on the host to the client certificate used to authenticate to the registry.
	CertFile string `json:"certFile,omitempty"`
	// KeyFile is the path on the host to the client private key used to authenticate to the registry.
	KeyFile string `json:"keyFile,omitempty"`
	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// VaultHeader marks a value in a cluster configuration as Ansible Vault ciphertext produced by
// `ansible-vault encrypt_string`. Cargoship decrypts such a value at apply time, so anything
// that inspects the plaintext has to wait until then.
const VaultHeader = "$ANSIBLE_VAULT"

// IsVaultEncrypted reports whether value is Ansible Vault ciphertext rather than a plain value.
func IsVaultEncrypted(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), VaultHeader)
}

// Validate returns an error when the TLS settings cannot be applied. A nil receiver is valid:
// TLS is optional. name identifies the registry in the error.
func (t *ZarfClusterRegistryTLS) Validate(name ZarfClusterRegistrieName) error {
	if t == nil {
		return nil
	}
	if t.CA != "" && t.CAFile != "" {
		return fmt.Errorf("registry %q: set tls.ca or tls.caFile, not both", name)
	}
	// A vault-encrypted CA is still ciphertext at this point. It is checked again once apply
	// decrypts it, which is the first moment there is a certificate to look at.
	if t.CA != "" && !IsVaultEncrypted(t.CA) {
		block, _ := pem.Decode([]byte(t.CA))
		if block == nil || block.Type != "CERTIFICATE" {
			return fmt.Errorf("registry %q: tls.ca is not a PEM-encoded certificate", name)
		}
	}
	if (t.CertFile == "") != (t.KeyFile == "") {
		return fmt.Errorf("registry %q: tls.certFile and tls.keyFile go together", name)
	}
	return nil
}

// ZarfClusterRegistryAuth holds the credentials for a container registry.
// Username, Password, and Token may each be given in plaintext, or as an
// Ansible Vault-encrypted string (the output of `ansible-vault encrypt_string`,
// starting with "$ANSIBLE_VAULT"), in which case cargoship decrypts it at apply
// time using the vault password given via --vault-password-file.
type ZarfClusterRegistryAuth struct {
	// Username is the login name for the remote registry.
	Username string `json:"user,omitempty" jsonschema:"example=myuser,example=$ANSIBLE_VAULT;1.1;AES256..."`
	// Password is the login secret for the remote registry.
	Password string `json:"pass,omitempty" jsonschema:"example=hunter2,example=$ANSIBLE_VAULT;1.1;AES256..."`
	// Token authenticates to the remote registry instead of a username and password.
	Token string `json:"token,omitempty" jsonschema:"example=abc123,example=$ANSIBLE_VAULT;1.1;AES256..."`
}

// endpointSchemeRegex matches a URL that already names a scheme, e.g. "https://" or "http://".
var endpointSchemeRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*://`)

// ZarfClusterRegistryProxy redirects pulls for a registry to a different URL.
type ZarfClusterRegistryProxy struct {
	// URL is the registry address the engine pulls from instead of the original registry.
	URL string `json:"url"`
	// Rewrite maps a regex pattern to a replacement, transforming the image name (not the tag)
	// before it is pulled from this mirror. See https://docs.rke2.io/install/private_registry#rewrites
	Rewrite map[string]string `json:"rewrite,omitempty"`
}

// validateRegistryName returns an error when a name could never match an image reference. An
// engine matches its registry configuration against the registry part of an image reference --
// "docker.io", "ghcr.io", "nexus.example.com:5000" -- so a name carrying a scheme, a repository
// path, or whitespace matches nothing, and the configuration written under it is dead weight
// nobody hears about again. "*" is the one exception: engines read it as "every registry".
func validateRegistryName(name ZarfClusterRegistrieName) error {
	value := string(name)
	switch {
	case value == "":
		return errors.New("registry: name is required")
	case value == "*":
		return nil
	case strings.Contains(value, "://"):
		return fmt.Errorf("registry %q: name is a registry host, not a URL -- drop the scheme", name)
	case strings.ContainsAny(value, "/"):
		return fmt.Errorf("registry %q: name is a registry host, not a repository path -- drop everything after the host", name)
	case strings.TrimSpace(value) != value || strings.ContainsAny(value, " \t"):
		return fmt.Errorf("registry %q: name contains whitespace", name)
	}
	return nil
}

// Validate returns an error when the proxy settings cannot be applied. A nil receiver is valid:
// a registry may be configured without redirecting pulls away from it. name identifies the
// registry in the error.
func (p *ZarfClusterRegistryProxy) Validate(name ZarfClusterRegistrieName) error {
	if p == nil {
		return nil
	}
	if p.URL == "" {
		return fmt.Errorf("registry %q: proxy.url is required when proxy is set", name)
	}

	endpoint := p.URL
	if !endpointSchemeRegex.MatchString(endpoint) {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	switch {
	case err != nil:
		return fmt.Errorf("registry %q: proxy.url %q is not a URL: %w", name, p.URL, err)
	case u.Host == "":
		return fmt.Errorf("registry %q: proxy.url %q has no host", name, p.URL)
	case u.Scheme != "http" && u.Scheme != "https":
		return fmt.Errorf("registry %q: proxy.url %q uses scheme %q, want http or https", name, p.URL, u.Scheme)
	case u.User != nil:
		return fmt.Errorf("registry %q: proxy.url carries credentials -- put them in auth instead", name)
	}

	// A rewrite is a Go regular expression the engine compiles on the node. One that does not
	// compile there fails the pull rather than the parse, which is a long way from here.
	for pattern, replacement := range p.Rewrite {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("registry %q: proxy.rewrite pattern %q is not a valid regular expression: %w", name, pattern, err)
		}
		if replacement == "" {
			return fmt.Errorf("registry %q: proxy.rewrite pattern %q has an empty replacement", name, pattern)
		}
	}
	return nil
}

// Validate returns an error when the credentials cannot be used as given. name identifies the
// registry in the error.
func (a ZarfClusterRegistryAuth) Validate(name ZarfClusterRegistrieName) error {
	// Basic auth is a pair. Half of one authenticates nothing, and the engine reports it as a
	// 401 from the registry rather than as the missing half it is.
	if a.Username != "" && a.Password == "" {
		return fmt.Errorf("registry %q: auth.user is set without auth.pass", name)
	}
	if a.Password != "" && a.Username == "" {
		return fmt.Errorf("registry %q: auth.pass is set without auth.user", name)
	}
	return nil
}

// ZarfClusterFiles defines a file to write to a host.
type ZarfClusterFiles struct {
	// Name identifies the file in the cluster configuration.
	Name string `json:"name"`
	// Source is the local path or URL cargoship reads the file from.
	Source string `json:"src,omitempty"`
	// Destination is the path on the host where cargoship writes the file.
	Destination string `json:"dst,omitempty"`
	// DestinationDirectory is the directory on the host where cargoship writes the file.
	DestinationDirectory string `json:"dstDir,omitempty"`
	// Permission sets the file mode cargoship applies on the host.
	Permission string `json:"perm,omitempty"`
	// User identifies the file owner on the host.
	User string `json:"user,omitempty" jsonschema:"example=root"`
	// Group identifies the file group on the host.
	Group string `json:"group,omitempty" jsonschema:"example=root"`
	// Data holds inline content for the file, as an alternative to Source.
	Data string `json:"data,omitempty"`
}
