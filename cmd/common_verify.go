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

	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/pkg/signing"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	zlang "github.com/zarf-dev/zarf/src/config/lang"
)

// flagGroupAnnotation is the pflag annotation key used to assign a flag to a named
// usage section. Flags carrying this annotation are rendered under their group title
// by the custom usage template instead of the default "Flags:" block.
const flagGroupAnnotation = "cargoship_flag_group"

// verifyFlagGroupTitle is the usage section title for package verification flags.
const verifyFlagGroupTitle = "Verification Flags"

// verifyKey is read directly from viper, never through resolvedConfig: it needs
// v.IsSet to distinguish "unset" from "set to the zero value", which a struct field
// cannot represent. It is excluded from Unmarshal (mapstructure:"-" in config.go), so
// configPath falls back to its json tag -- see configPath's doc comment in viper.go.
var verifyKey = configPath("DistroOpts", "Verify")

// annotateFlagGroup tags every flag in fs with the given usage-section title.
func annotateFlagGroup(fs *pflag.FlagSet, title string) {
	fs.VisitAll(func(f *pflag.Flag) {
		err := fs.SetAnnotation(f.Name, flagGroupAnnotation, []string{title})
		if err != nil {
			panic(err)
		}
	})
}

// packageVerifyFlags holds the package signature verification flags. Embed this in
// any command options struct that performs a package load operation. To add a new
// verification flag in the future, add the field here, register it in
// newVerifyFlagSet, then update buildVerifyBlobOptions.
type packageVerifyFlags struct {
	publicKeyPath string
	verify        verifyMode
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

// newVerifyFlagSet creates a pflag.FlagSet containing the package verification flags.
func newVerifyFlagSet(f *packageVerifyFlags) *pflag.FlagSet {
	fs := pflag.NewFlagSet("verify", pflag.ContinueOnError)

	fs.StringVarP(&f.publicKeyPath, "key", "k", resolvedConfig.DistroOpts.PublicKey, zlang.CmdPackageFlagFlagPublicKey)
	f.verify = verifyModeIfPossible
	fs.VarP(&f.verify, "verify", "", lang.CmdPackageFlagVerify)

	annotateFlagGroup(fs, verifyFlagGroupTitle)
	return fs
}

// addVerifyFlags registers the verification flag set on cmd. Use this for all
// commands that load packages. The sign and verify commands are exceptions - they
// register --key manually with a command-specific usage string.
func addVerifyFlags(cmd *cobra.Command, f *packageVerifyFlags) {
	cmd.Flags().AddFlagSet(newVerifyFlagSet(f))
}

// buildVerifyBlobOptions builds signing.VerifyBlobOptions from the verification flags.
func (f *packageVerifyFlags) buildVerifyBlobOptions() *signing.VerifyBlobOptions {
	opts := signing.DefaultVerifyBlobOptions()
	opts.Key = f.publicKeyPath
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
