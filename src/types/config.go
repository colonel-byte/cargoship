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

// Package types is a little bit of a hacky way to generate the cargo-ship-config jsonschema
package types

import (
	"github.com/invopop/jsonschema"
	orderedmap "github.com/pb33f/ordered-map/v2"
)

// CommonRegistries are suggested, non-exhaustive registry names used in generated
// schemas -- editors with YAML/JSON schema support (e.g. the redhat.vscode-yaml
// extension) offer them as autocomplete, for RegistryOverrideMap's registry_override
// keys here and for ZarfClusterRegistrieName's registry name field in the cluster API.
var CommonRegistries = []string{
	"docker.io",
	"ghcr.io",
	"quay.io",
	"gcr.io",
	"registry.k8s.io",
	"public.ecr.aws",
}

// DistroConfig holds the values for the `.`, or root, section of the config file
type DistroConfig struct {
	// CachePath is the folder where oras artifacts are stored
	CachePath string `json:"zarf_cache,omitempty" mapstructure:"zarf_cache"`
	// DistroOpts are various options used by the command
	DistroOpts DistroOptions `json:"distro,omitempty" mapstructure:"distro"`
	// LogFormat how the logs are displayed well running
	LogFormat string `json:"log_format,omitempty" mapstructure:"log_format" jsonschema:"enum=console,enum=json,enum=dev,default=console"`
	// LogLevel the level of logs that will be displayed
	LogLevel string `json:"log_level,omitempty" mapstructure:"log_level" jsonschema:"enum=warn,enum=info,enum=debug,enum=trace,default=info"`
	// NoColor whether to disable terminal color codes in logging and stdout prints
	NoColor bool `json:"no_color,omitempty" mapstructure:"no_color"`
	// LogFile enables always writing a full-verbosity debug log to a file, regardless of LogLevel
	LogFile bool `json:"log_file,omitempty" mapstructure:"log_file"`
	// Architecture the CPU architecture to use for OCI operations
	Architecture string `json:"architecture,omitempty" mapstructure:"architecture"`
	// TempDirectory the directory where we store stuff before deleting them
	TempDirectory string `json:"tmp_dir,omitempty" mapstructure:"tmp_dir" jsonschema:"default=/tmp"`
	// Timeout the longest we will run long ran tasks before failing
	Timeout string `json:"timeout,omitempty" mapstructure:"timeout" jsonschema:"default=20m"`
}

// DistroOptions holds the values for the `.distro` section of the config file
type DistroOptions struct {
	// CreateOpts are options used by the create subcommand
	CreateOpts DistroCreateOptions `json:"create,omitempty" mapstructure:"create"`
	// PublishOpts are options used by the publish subcommand
	PublishOpts DistroPublishOptions `json:"publish,omitempty" mapstructure:"publish"`
	// DeployOpts are options used by the deploy subcommand
	DeployOpts DistroDeployOptions `json:"deploy,omitempty" mapstructure:"deploy"`
	// ApplyOpts are options used by the apply subcommand
	ApplyOpts ApplyOptions `json:"apply,omitempty" mapstructure:"apply"`
	// ResetOptions are options used by the reset subcommand
	ResetOpts ResetOptions `json:"reset,omitempty" mapstructure:"reset"`
	// OCIConcurrency is how many concurrent oci artifacts that will be pushed at a time
	OCIConcurrency int `json:"oci_concurrency,omitempty" mapstructure:"oci_concurrency"`
	// Concurrency how many nodes we will try to interact with at a time, 0 means that all nodes will be done at once
	Concurrency int `json:"concurrency,omitempty" mapstructure:"concurrency" jsonschema:"minimum=0"`
	// FAPolicyd whether we will update hosts with fapolicyd
	FAPolicyd bool `json:"fapolicyd,omitempty" mapstructure:"fapolicyd" jsonschema:"default=true"`
	// FirewallUpdate whether we will update the host firewall
	FirewallUpdate bool `json:"firewall,omitempty" mapstructure:"firewall" jsonschema:"default=true"`
	// HostUpdate whether we will update the etc host file
	HostUpdate bool `json:"hosts,omitempty" mapstructure:"hosts" jsonschema:"default=true"`
	// LabelNodes whether we will check and add the node-role.kubernetes.io/<profile> label on nodes
	LabelNodes bool `json:"label_nodes,omitempty" mapstructure:"label_nodes" jsonschema:"default=true"`
	// UpdateKubeConfig whether we will update the local kubeconfig file with the admin creds for the cluster
	UpdateKubeConfig bool `json:"kubeconfig,omitempty" mapstructure:"kubeconfig" jsonschema:"default=true"`
	// WorkerConcurrency number of worker nodes that will be upgraded at once, as a fixed count
	// ("5") or a percentage of the batch ("25%")
	WorkerConcurrency string `json:"worker_concurrency,omitempty" mapstructure:"worker_concurrency" jsonschema:"oneof_type=string;integer" jsonschema_extras:"examples=1,examples=5,examples=25%,examples=100%"`
	// Retry number of retries we will try
	Retry int `json:"retry,omitempty" mapstructure:"retry" jsonschema:"minimum=0"`
	// Type of distro we are interacting with
	Type string `json:"type,omitempty" mapstructure:"type" jsonschema:"enum=rke2,enum=k3s"`
	// Output the folder that we will create the distro tar balls in
	Output string `json:"output,omitempty" mapstructure:"output"`
	// Verify the Cargoship package signature
	Verify string `json:"verify,omitempty" mapstructure:"-" jsonschema:"enum=never,enum=if-possible,enum=always"` // mapstructure:"-": common_verify.go needs v.IsSet to distinguish "unset" from "set to zero value" (never read from the resolved struct)
	// PublicKey path to public key file for validating signed packages
	PublicKey string `json:"public_key,omitempty" mapstructure:"public_key"`
	// CertificateIdentity required identity claim in the signing certificate
	CertificateIdentity string `json:"certificate_identity,omitempty" mapstructure:"certificate_identity"`
	// CertificateIdentityRegexp equired identity claim in the signing certificate, allows usage of regex
	CertificateIdentityRegexp string `json:"certificate_identity_regexp,omitempty" mapstructure:"certificate_identity_regexp"`
	// CertificateOIDCIssuer required OIDC issuer claim in the signing certificate
	CertificateOIDCIssuer string `json:"certificate_oidc_issuer,omitempty" mapstructure:"certificate_oidc_issuer"`
	// CertificateOIDCIssuerRegexp required OIDC issuer claim in the signing certificate, allows usage of regex
	CertificateOIDCIssuerRegexp string `json:"certificate_oidc_issuer_regexp,omitempty" mapstructure:"certificate_oidc_issuer_regexp"`
	// TrustedRoot path to a Sigstore TrustedRoot JSON
	TrustedRoot string `json:"trusted_root,omitempty" mapstructure:"trusted_root"`
	// InsecureIgnoreTLog
	InsecureIgnoreTLog string `json:"insecure_ignore_tlog,omitempty" mapstructure:"-"` // mapstructure:"-": same v.IsSet reasoning as Verify above, plus this is really a bool (read via v.GetBool) despite the string type
	// UseSignedTimestamps verify RFC3161 signed timestamps in the bundle. Auto-enabled when the bundle contains TSA timestamp data.
	UseSignedTimestamps string `json:"use_signed_timestamps,omitempty" mapstructure:"-"` // mapstructure:"-": really a bool (read via v.GetBool in common_verify.go) despite the string type
}

// DistroCreateOptions holds the values for the `.distro.create` section of the config file
type DistroCreateOptions struct {
	// RegistryOverride maps a source registry to the registry cargoship uses instead
	// when pulling images, for example {"docker.io": "mirror.example.com"}
	RegistryOverride RegistryOverrideMap `json:"registry_override,omitempty" mapstructure:"-"` // mapstructure:"-": viper always splits a map key on "." when merging its settings tree, so a domain key like "docker.io" gets silently corrupted into a nested map; read directly from the config file instead (see initViper in cmd/viper.go)
}

// DistroPublishOptions holds the values for the `.distro.publish` section of the config file
type DistroPublishOptions struct {
	// SigningKey is the path to the private key, a Cosign-supported key provider, used to sign, or re-sign, the package
	SigningKey string `json:"signing_key,omitempty" mapstructure:"signing_key" jsonschema:"example=/home/runner/.cosign/sign.key,example=env://[ENV_VAR],example=awskms://[ENDPOINT]/[ID/ALIAS/ARN],example=openbao://[KEY]"`
	// SigningKeyPassword the password for the private key used for signing
	SigningKeyPassword string `json:"signing_key_password,omitempty" mapstructure:"signing_key_password"`
}

// DistroDeployOptions holds the values for the `.distro.deploy` section of the config file
type DistroDeployOptions struct {
	// Retries how many times we will try to push a package
	Retries int `json:"retries,omitempty" mapstructure:"retries"`
}

// ApplyOptions holds the values for the `.distro.apply` section of the config file
type ApplyOptions struct{}

// ResetOptions holds the values for the `.distro.reset` section of the config file
type ResetOptions struct{}

// RegistryOverrideMap maps a source registry to the registry cargoship uses instead.
// It's a named type (rather than a bare map[string]string) solely so it can implement
// JSONSchemaExtend below and suggest common registries in the generated schema; the
// config file is not restricted to those.
type RegistryOverrideMap map[string]string

// JSONSchemaExtend adds CommonRegistries to the schema's properties, alongside the
// additionalProperties the reflector already set for the map[string]string element
// type, so the suggestions are additive and don't restrict which keys are allowed.
func (RegistryOverrideMap) JSONSchemaExtend(s *jsonschema.Schema) {
	suggestions := orderedmap.New[string, *jsonschema.Schema]()
	for _, registry := range CommonRegistries {
		suggestions.Set(registry, &jsonschema.Schema{Type: "string"})
	}
	s.Properties = suggestions
}
