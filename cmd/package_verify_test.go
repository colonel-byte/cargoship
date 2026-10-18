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

package cmd

import (
	"io"
	"testing"

	"github.com/spf13/cobra"
)

func TestPackageVerifyRegistersTheVerificationFlags(t *testing.T) {
	cmd := newPackageVerifyCommand()

	// --verify is deliberately absent: verification is the command's only job, so
	// there is no mode to opt out of it.
	if cmd.Flags().Lookup("key") == nil {
		t.Error("newPackageVerifyCommand() is missing the --key flag")
	}
	if f := cmd.Flags().Lookup("verify"); f != nil {
		t.Error("newPackageVerifyCommand() registers --verify, which it should not")
	}
}

// TestPackageVerifyHasNoKeylessFlags is the regression test for the keyless removal.
// A flag that lingers after the code behind it is gone either does nothing or does
// something other than what it says, and nothing else in the suite notices: every
// other sign and verify test drives a key, so all of them passed unchanged while
// keyless was being removed.
func TestPackageVerifyHasNoKeylessFlags(t *testing.T) {
	for _, cmd := range []*cobra.Command{
		newPackageVerifyCommand(),
		newPackageSignCommand(),
	} {
		for _, name := range []string{
			"certificate-identity",
			"certificate-identity-regexp",
			"certificate-oidc-issuer",
			"certificate-oidc-issuer-regexp",
			"trusted-root",
			"insecure-ignore-tlog",
			"use-signed-timestamps",
			"keyless",
			"identity-token",
			"fulcio-url",
			"fulcio-auth-flow",
			"oidc-issuer",
			"oidc-client-id",
			"rekor-url",
			"tlog-upload",
			"tsa-server-url",
		} {
			if f := cmd.Flags().Lookup(name); f != nil {
				t.Errorf("%s registers --%s, which was removed with keyless signing", cmd.Name(), name)
			}
		}
	}
}

func TestPackageVerifyRequiresExactlyOneArgument(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "no arguments",
			args: []string{},
		},
		{
			name: "two arguments",
			args: []string{"a.tar.zst", "b.tar.zst"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newPackageVerifyCommand()
			cmd.RunE = func(_ *cobra.Command, _ []string) error { return nil }
			cmd.SetArgs(tt.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err == nil {
				t.Fatalf("Execute(%v) = nil error, want an argument-count error", tt.args)
			}
		})
	}
}
