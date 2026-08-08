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

// DistroConfig holds the values for the `.`, or root, section of the config file
type DistroConfig struct {
	// CachePath is the folder where oras artifacts are stored
	CachePath string `json:"zarf_cache,omitempty"`
	// DistroOpts are various options used by the command
	DistroOpts DistroOptions `json:"distro,omitempty"`
	// LogFormat how the logs are displayed well running
	LogFormat string `json:"log_format,omitempty" jsonschema:"enum=console,enum=json,enum=dev,default=console"`
	// LogLevel the level of logs that will be displayed
	LogLevel string `json:"log_level,omitempty" jsonschema:"enum=warn,enum=info,enum=debug,enum=trace,default=info"`
	// TempDirectory the directory where we store stuff before deleting them
	TempDirectory string `json:"tmp_dir,omitempty" jsonschema:"default=/tmp"`
	// Timeout the longest we will run long ran tasks before failing
	Timeout string `json:"timeout,omitempty" jsonschema:"default=20m"`
}

// DistroOptions holds the values for the `.distro` section of the config file
type DistroOptions struct {
	// CreateOpts are options used by the create subcommand
	CreateOpts DistroCreateOptions `json:"create,omitempty"`
	// PublishOpts are options used by the publish subcommand
	PublishOpts DistroPublishOptions `json:"publish,omitempty"`
	// DeployOpts are options used by the deploy subcommand
	DeployOpts DistroDeployOptions `json:"deploy,omitempty"`
	// ApplyOpts are options used by the apply subcommand
	ApplyOpts ApplyOptions `json:"apply,omitempty"`
	// ResetOptions are options used by the reset subcommand
	ResetOpts ResetOptions `json:"reset,omitempty"`
	// OCIConcurrency is how many concurrent oci artifacts that will be pushed at a time
	OCIConcurrency int `json:"oci_concurrency,omitempty"`
	// Concurrency how many nodes we will try to interact with at a time, 0 means that all nodes will be done at once
	Concurrency int `json:"concurrency,omitempty" jsonschema:"minimum=0"`
	// FAPolicyd whether we will update hosts with fapolicyd
	FAPolicyd bool `json:"fapolicy,omitempty" jsonschema:"default=true"`
	// FirewallUpdate whether we will update the host firewall
	FirewallUpdate bool `json:"firewall_update,omitempty" jsonschema:"default=true"`
	// HostUpdate whether we will update the etc host file
	HostUpdate bool `json:"host_update,omitempty" jsonschema:"default=true"`
	// WorkerConcurrency number of worker nodes that will be upgraded at once
	WorkerConcurrency int `json:"worker_concurrency,omitempty" jsonschema:"minimum=0,maximum=1000"`
	// Retry number of retries we will try
	Retry int `json:"retry,omitempty" jsonschema:"minimum=0"`
	// Type of distro we are interacting with
	Type string `json:"type,omitempty" jsonschema:"enum=rke2,enum=k3s"`
	// Output the folder that we will create the distro tar balls in
	Output string `json:"output,omitempty"`
	// Verify the Cargoship package signature
	Verify string `json:"verify,omitempty" jsonschema:"enum=never,enum=if-possible,enum=always"`
	// PublicKey path to public key file for validating signed packages
	PublicKey string `json:"public_key,omitempty"`
	// CertificateIdentity required identity claim in the signing certificate
	CertificateIdentity string `json:"certificate_identity,omitempty"`
	// CertificateIdentityRegexp equired identity claim in the signing certificate, allows usage of regex
	CertificateIdentityRegexp string `json:"certificate_identity_regexp,omitempty"`
	// CertificateOIDCIssuer required OIDC issuer claim in the signing certificate
	CertificateOIDCIssuer string `json:"certificate_oidc_issuer,omitempty"`
	// CertificateOIDCIssuerRegexp required OIDC issuer claim in the signing certificate, allows usage of regex
	CertificateOIDCIssuerRegexp string `json:"certificate_oidc_issuer_regexp,omitempty"`
	// TrustedRoot path to a Sigstore TrustedRoot JSON
	TrustedRoot string `json:"trusted_root,omitempty"`
	// InsecureIgnoreTLog
	InsecureIgnoreTLog string `json:"insecure_ignore_tlog,omitempty"`
	// UseSignedTimestamps verify RFC3161 signed timestamps in the bundle. Auto-enabled when the bundle contains TSA timestamp data.
	UseSignedTimestamps string `json:"use_signed_timestamps,omitempty"`
}

// DistroCreateOptions holds the values for the `.distro.create` section of the config file
type DistroCreateOptions struct {
	// SkipSBOM whether we will scan the images or files
	SkipSBOM bool `json:"skip_sbom,omitempty"`
}

// DistroPublishOptions holds the values for the `.distro.publish` section of the config file
type DistroPublishOptions struct {
	// SigningKey is the path to the private key, a Cosign-supported key provider, used to sign, or re-sign, the package
	SigningKey string `json:"signing_key,omitempty" jsonschema:"example=/home/runner/.cosign/sign.key,example=env://[ENV_VAR],example=awskms://[ENDPOINT]/[ID/ALIAS/ARN],example=openbao://[KEY]"`
	// SigningKeyPassword the password for the private key used for signing
	SigningKeyPassword string `json:"signing_key_password,omitempty"`
}

// DistroDeployOptions holds the values for the `.distro.deploy` section of the config file
type DistroDeployOptions struct {
	// Retries how many times we will try to push a package
	Retries int `json:"retries,omitempty"`
}

// ApplyOptions holds the values for the `.distro.apply` section of the config file
type ApplyOptions struct{}

// ResetOptions holds the values for the `.distro.reset` section of the config file
type ResetOptions struct{}
