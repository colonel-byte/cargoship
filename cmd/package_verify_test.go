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
	for _, name := range []string{
		"key",
		"certificate-identity",
		"certificate-identity-regexp",
		"certificate-oidc-issuer",
		"certificate-oidc-issuer-regexp",
		"trusted-root",
		"insecure-ignore-tlog",
		"use-signed-timestamps",
	} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("newPackageVerifyCommand() is missing the --%s flag", name)
		}
	}
	if f := cmd.Flags().Lookup("verify"); f != nil {
		t.Error("newPackageVerifyCommand() registers --verify, which it should not")
	}
}

func TestPackageVerifyRejectsKeyWithKeylessFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "key with certificate identity",
			args: []string{"pkg.tar.zst", "--key", "./cosign.pub", "--certificate-identity", "signer@example.com"},
		},
		{
			name: "key with certificate identity regexp",
			args: []string{"pkg.tar.zst", "--key", "./cosign.pub", "--certificate-identity-regexp", ".*"},
		},
		{
			name: "key with oidc issuer",
			args: []string{"pkg.tar.zst", "--key", "./cosign.pub", "--certificate-oidc-issuer", "https://example.com"},
		},
		{
			name: "identity and its regexp together",
			args: []string{"pkg.tar.zst", "--certificate-identity", "signer@example.com", "--certificate-identity-regexp", ".*"},
		},
		{
			name: "oidc issuer and its regexp together",
			args: []string{"pkg.tar.zst", "--certificate-oidc-issuer", "https://example.com", "--certificate-oidc-issuer-regexp", ".*"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newPackageVerifyCommand()
			// Replace RunE so a passing flag check does not go on to load a package.
			cmd.RunE = func(_ *cobra.Command, _ []string) error { return nil }
			cmd.SetArgs(tt.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err == nil {
				t.Fatalf("Execute(%v) = nil error, want a mutual-exclusion error", tt.args)
			}
		})
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
