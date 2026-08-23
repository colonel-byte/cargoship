// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
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

package cmd

import (
	"fmt"

	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	zlang "github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/signing"
)

// flagGroupAnnotation is the pflag annotation key used to assign a flag to a named
// usage section. Flags carrying this annotation are rendered under their group title
// by the custom usage template instead of the default "Flags:" block.
const flagGroupAnnotation = "cargoship_flag_group"

// verifyFlagGroupTitle is the usage section title for package verification flags.
const verifyFlagGroupTitle = "Verification Flags"

// These three keys are read directly from viper, never through resolvedConfig: verify
// and insecureIgnoreTlogKey need v.IsSet to distinguish "unset" from "set to zero
// value", which a struct field can't represent, and useSignedTimestampsKey's real
// value is a bool (read via v.GetBool) even though the DistroOptions field it maps to
// is typed as a string (mirroring the config-file schema). All three are excluded from
// Unmarshal (mapstructure:"-" in config.go), so configPath falls back to their json
// tag -- see configPath's doc comment in viper.go.
var (
	verifyKey              = configPath("DistroOpts", "Verify")
	insecureIgnoreTlogKey  = configPath("DistroOpts", "InsecureIgnoreTLog")
	useSignedTimestampsKey = configPath("DistroOpts", "UseSignedTimestamps")
)

// annotateFlagGroup tags every flag in fs with the given usage-section title.
func annotateFlagGroup(fs *pflag.FlagSet, title string) {
	fs.VisitAll(func(f *pflag.Flag) {
		err := fs.SetAnnotation(f.Name, flagGroupAnnotation, []string{title})
		if err != nil {
			panic(err)
		}
	})
}

// packageVerifyFlags holds all package signature and keyless verification flags.
// Embed this in any command options struct that performs a package load operation.
// To add a new verification flag in the future, add the field here and register it
// in newVerifyFlagSet / newKeylessVerifyFlagSet, then update buildVerifyBlobOptions.
type packageVerifyFlags struct {
	publicKeyPath               string
	verify                      verifyMode
	certificateIdentity         string
	certificateIdentityRegexp   string
	certificateOIDCIssuer       string
	certificateOIDCIssuerRegexp string
	trustedRoot                 string
	insecureIgnoreTlog          bool
	useSignedTimestamps         bool
}

// verifyMode is the value type for the --verify flag.
type verifyMode string

const (
	verifyModeNever      verifyMode = "never"
	verifyModeIfPossible verifyMode = "if-possible"
	verifyModeAlways     verifyMode = "always"
)

// Set implements pflag.Value. Accepts the three canonical values and legacy bool strings.
func (m *verifyMode) Set(s string) error {
	switch verifyMode(s) {
	case verifyModeNever, verifyModeIfPossible, verifyModeAlways:
		*m = verifyMode(s)
	// Accept legacy bool values from viper configs written before the enum was introduced.
	case "true":
		*m = verifyModeAlways
	case "false":
		*m = verifyModeIfPossible
	default:
		return fmt.Errorf("invalid --verify value %q (must be never, if-possible, or always)", s)
	}
	return nil
}

// String implements pflag.Value.
func (m verifyMode) String() string { return string(m) }

// Type implements pflag.Value.
func (m verifyMode) Type() string { return "verifyMode" }

// toStrategy converts the flag value to the layout.VerificationStrategy used internally.
func (m verifyMode) toStrategy() layout.VerificationStrategy {
	switch m {
	case verifyModeNever:
		return layout.VerifyNever
	case verifyModeAlways:
		return layout.VerifyAlways
	default:
		return layout.VerifyIfPossible
	}
}

// newKeylessVerifyFlagSet creates a pflag.FlagSet containing only the 7 keyless
// verification flags (certificate identity/issuer, trusted root, tlog, timestamps).
// Used by newVerifyFlagSet and directly by the sign command, which registers --key
// separately with a command-specific usage string.
func newKeylessVerifyFlagSet(v *viper.Viper, f *packageVerifyFlags) *pflag.FlagSet {
	fs := pflag.NewFlagSet("keyless-verify", pflag.ContinueOnError)

	fs.StringVar(&f.certificateIdentity, "certificate-identity",
		resolvedConfig.DistroOpts.CertificateIdentity, zlang.CmdPackageVerifyFlagCertificateIdentity)
	fs.StringVar(&f.certificateIdentityRegexp, "certificate-identity-regexp",
		resolvedConfig.DistroOpts.CertificateIdentityRegexp, zlang.CmdPackageVerifyFlagCertificateIdentityRegexp)
	fs.StringVar(&f.certificateOIDCIssuer, "certificate-oidc-issuer",
		resolvedConfig.DistroOpts.CertificateOIDCIssuer, zlang.CmdPackageVerifyFlagCertificateOIDCIssuer)
	fs.StringVar(&f.certificateOIDCIssuerRegexp, "certificate-oidc-issuer-regexp",
		resolvedConfig.DistroOpts.CertificateOIDCIssuerRegexp, zlang.CmdPackageVerifyFlagCertificateOIDCIssuerRegexp)
	fs.StringVar(&f.trustedRoot, "trusted-root",
		resolvedConfig.DistroOpts.TrustedRoot, zlang.CmdPackageVerifyFlagTrustedRoot)

	ignoreTlogDefault := true
	if v.IsSet(insecureIgnoreTlogKey) {
		ignoreTlogDefault = v.GetBool(insecureIgnoreTlogKey)
	}
	fs.BoolVar(&f.insecureIgnoreTlog, "insecure-ignore-tlog", ignoreTlogDefault,
		zlang.CmdPackageVerifyFlagInsecureIgnoreTlog)
	fs.BoolVar(&f.useSignedTimestamps, "use-signed-timestamps",
		v.GetBool(useSignedTimestampsKey), zlang.CmdPackageVerifyFlagUseSignedTimestamps)

	annotateFlagGroup(fs, verifyFlagGroupTitle)
	return fs
}

// newVerifyFlagSet creates a pflag.FlagSet containing all 10 package verification flags
func newVerifyFlagSet(v *viper.Viper, f *packageVerifyFlags) *pflag.FlagSet {
	fs := pflag.NewFlagSet("verify", pflag.ContinueOnError)

	fs.StringVarP(&f.publicKeyPath, "key", "k", resolvedConfig.DistroOpts.PublicKey, zlang.CmdPackageFlagFlagPublicKey)
	f.verify = verifyModeIfPossible
	fs.VarP(&f.verify, "verify", "", lang.CmdPackageFlagVerify)
	fs.Lookup("verify").NoOptDefVal = string(verifyModeAlways)

	fs.AddFlagSet(newKeylessVerifyFlagSet(v, f))
	annotateFlagGroup(fs, verifyFlagGroupTitle)
	return fs
}

// addVerifyFlags registers the full verification flag set on cmd and marks the
// key/keyless flags mutually exclusive. Use this for all commands that load packages.
// The sign and verify commands are exceptions — they register --key manually and use
// newKeylessVerifyFlagSet directly, then call markVerifyFlagsMutuallyExclusive themselves.
func addVerifyFlags(cmd *cobra.Command, v *viper.Viper, f *packageVerifyFlags) {
	cmd.Flags().AddFlagSet(newVerifyFlagSet(v, f))
	markVerifyFlagsMutuallyExclusive(cmd)
}

// markVerifyFlagsMutuallyExclusive registers --key vs keyless flag mutual exclusions on cmd.
// Must be called after cmd.Flags().AddFlagSet so all flags are present on the command.
func markVerifyFlagsMutuallyExclusive(cmd *cobra.Command) {
	cmd.MarkFlagsMutuallyExclusive("key", "certificate-identity")
	cmd.MarkFlagsMutuallyExclusive("key", "certificate-identity-regexp")
	cmd.MarkFlagsMutuallyExclusive("key", "certificate-oidc-issuer")
	cmd.MarkFlagsMutuallyExclusive("key", "certificate-oidc-issuer-regexp")
	cmd.MarkFlagsMutuallyExclusive("certificate-identity", "certificate-identity-regexp")
	cmd.MarkFlagsMutuallyExclusive("certificate-oidc-issuer", "certificate-oidc-issuer-regexp")
}

// buildVerifyBlobOptions builds signing.VerifyBlobOptions from the verification flags,
// applying the tlog auto-disable logic for keyless identity verification.
func (f *packageVerifyFlags) buildVerifyBlobOptions(cmd *cobra.Command, v *viper.Viper) *signing.VerifyBlobOptions {
	opts := signing.DefaultVerifyBlobOptions()
	opts.Key = f.publicKeyPath
	opts.CertVerify.CertIdentity = f.certificateIdentity
	opts.CertVerify.CertIdentityRegexp = f.certificateIdentityRegexp
	opts.CertVerify.CertOidcIssuer = f.certificateOIDCIssuer
	opts.CertVerify.CertOidcIssuerRegexp = f.certificateOIDCIssuerRegexp
	opts.CommonVerifyOptions.TrustedRootPath = f.trustedRoot
	opts.CommonVerifyOptions.IgnoreTlog = f.insecureIgnoreTlog
	opts.CommonVerifyOptions.UseSignedTimestamps = f.useSignedTimestamps

	// When a keyless identity is provided, require tlog verification by default so the
	// inclusion proof establishes when the signature was made. Honor any explicit override.
	// cmd may be nil when run() is called directly (e.g. from tests); in that case only
	// the viper config path is checked for an explicit override.
	hasKeylessIdentity := f.certificateIdentity != "" || f.certificateIdentityRegexp != ""
	tlogExplicit := v.IsSet(insecureIgnoreTlogKey)
	if cmd != nil {
		tlogExplicit = tlogExplicit || cmd.Flags().Changed("insecure-ignore-tlog")
	}
	if hasKeylessIdentity && !tlogExplicit {
		opts.CommonVerifyOptions.IgnoreTlog = false
	}
	return &opts
}

// preRunE is the cobra PreRunE handler for commands that embed packageVerifyFlags.
// It is promoted to embedding structs automatically, so no per-command wrapper is needed.
func (f *packageVerifyFlags) preRunE(cmd *cobra.Command, _ []string) error {
	// Apply viper default for --verify when the flag was not set on the CLI.
	// Accepts legacy bool values ("true"/"false") from existing configs.
	if !cmd.Flags().Changed("verify") && v.IsSet(verifyKey) {
		if err := f.verify.Set(v.GetString(verifyKey)); err != nil {
			return fmt.Errorf("invalid package.verify config value %q: %w", v.GetString(verifyKey), err)
		}
	}
	return nil
}
