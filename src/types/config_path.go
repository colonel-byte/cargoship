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

package types

const (
	// LoggingLevelDefault path in config
	LoggingLevelDefault = "info"
	// LogLevel path in config
	LogLevel = "log_level"
	// LogFormat path in config
	LogFormat = "log_format"
	// NoColor path in config
	NoColor = "no_color"
	// Architecture path in config
	Architecture = "architecture"
	// ZarfCache path in config
	ZarfCache = "zarf_cache"
	// TmpDir path in config
	TmpDir = "tmp_dir"
	// DistroOutput path in config
	DistroOutput = "distro.output"
	// DistroPublishSigningKey path in config
	DistroPublishSigningKey = "distro.publish.signing_key"
	// DistroPublishSigningKeyPassword path in config
	DistroPublishSigningKeyPassword = "distro.publish.signing_key_password"
	// DistroRetry path in config
	DistroRetry = "distro.retry"
	// DistroOCIConcurrency path in config
	DistroOCIConcurrency = "distro.oci_concurrency"
	// DistroCreateRegistryOverride path in config
	DistroCreateRegistryOverride = "distro.create.registry_override"
	// DistroCreateSkipSbom path in config
	DistroCreateSkipSbom = "distro.create.skip_sbom"
	// DistroConcurrency path in config
	DistroConcurrency = "distro.concurrency"
	// DistroWorkerConcurrency path in config
	DistroWorkerConcurrency = "distro.worker_concurrency"
	// DistroFAPolicy path in config
	DistroFAPolicy = "distro.fapolicy"
	// DistroUpdateHost path in config
	DistroUpdateHost = "distro.host_update"
	// DistroUpdateFirewall path in config
	DistroUpdateFirewall = "distro.firewall_update"
	// DistroType path in config
	DistroType = "distro.type"
	// DistroVerify path in config
	DistroVerify = "distro.verify"
	// DistroPublicKey path in config
	DistroPublicKey = "distro.public_key"
	// DistroCertificateIdentity path in config
	DistroCertificateIdentity = "distro.certificate_identity"
	// DistroCertificateIdentityRegexp path in config
	DistroCertificateIdentityRegexp = "distro.certificate_identity_regexp"
	// DistroCertificateOIDCIssuer path in config
	DistroCertificateOIDCIssuer = "distro.certificate_oidc_issuer"
	// DistroCertificateOIDCIssuerRegexp path in config
	DistroCertificateOIDCIssuerRegexp = "distro.certificate_oidc_issuer_regexp"
	// DistroTrustedRoot path in config
	DistroTrustedRoot = "distro.trusted_root"
	// DistroInsecureIgnoreTlog path in config
	DistroInsecureIgnoreTlog = "distro.insecure_ignore_tlog"
	// DistroUseSignedTimestamps path in config
	DistroUseSignedTimestamps = "distro.use_signed_timestamps"
)
