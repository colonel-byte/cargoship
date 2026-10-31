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

package receipt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// sensitiveFixture is a receipt from a plausible production run. Every identifying value in it is
// distinctive enough to grep for, which is what TestRedactLeavesNoSentinelBehind relies on.
const sensitiveFixture = "testdata/sensitive.json"

// sentinels are the values that must not survive redaction, each one a thing that would tell a
// stranger who the fleet belongs to or let them into it.
var sentinels = []string{
	"acme-prod-euw1",               // cluster name
	"api.acme-prod.internal",       // control-plane endpoint
	"jrivera",                      // operator
	"bastion-acme-prod",            // management node
	"/srv/acme-corp",               // paths carrying the customer name
	"acme-kc01.acme-prod.internal", // hostname
	"acme-kw01.acme-prod.internal", // hostname
	"10.44.3.11",                   // address
	"10.44.3.12",                   // address
	"ens192",                       // private interface
	"eth0",                         // private interface
	"K10SUPERSECRETJOINTOKEN",      // join token, inside a diff body
	"OLDTOKEN",                     // the other side of the same diff
	"prod-cluster.yaml",            // config path
}

// TestRedactLeavesNoSentinelBehind is the test that matters most here. It does not inspect
// fields: it serializes the whole redacted document and asserts that no identifying string from
// the original appears anywhere in it, which catches a leak through a field nobody thought about.
func TestRedactLeavesNoSentinelBehind(t *testing.T) {
	in, err := Read(sensitiveFixture)
	require.NoError(t, err)

	// Guard the fixture itself: if these stop appearing in the input, the test below passes for
	// the wrong reason.
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	for _, s := range sentinels {
		require.Contains(t, string(raw), s, "the fixture should contain %q before redaction", s)
	}

	out, err := json.Marshal(Redact(in))
	require.NoError(t, err)
	for _, s := range sentinels {
		require.NotContains(t, string(out), s, "%q survived redaction", s)
	}
}

// TestRedactKeepsWhatTriageNeeds is the other half. A redaction that drops everything is safe and
// useless; these are the fields that make the artifact worth attaching.
func TestRedactKeepsWhatTriageNeeds(t *testing.T) {
	in, err := Read(sensitiveFixture)
	require.NoError(t, err)

	got := Redact(in)

	require.True(t, got.Redacted)
	require.Equal(t, "1.0", got.SchemaVersion)
	require.Equal(t, "apply", got.Invocation.Command)
	require.Equal(t, "v1.2.3", got.Invocation.Version)
	require.Equal(t, "abc1234", got.Invocation.Commit)
	require.Equal(t, "11m12s", got.Invocation.Duration)

	require.Equal(t, 2, got.Cluster.HostCount)

	// A warning survives with its phase, path and reason, and with the same alias the phase
	// ledger gave that host: a kept file an operator has to go and look at is worth nothing
	// without knowing which host to look on.
	require.Len(t, got.Outcome.Warnings, 1)
	require.Equal(t, Warning{
		Phase:  "Upload files",
		Host:   "host-01",
		Path:   "/var/lib/rancher/rke2/agent/images/vendor-appliance.tar",
		Reason: "kept, unmanaged",
	}, got.Outcome.Warnings[0])

	require.NotNil(t, got.Package)
	require.Equal(t, "rke2", got.Package.Engine)
	require.Equal(t, "3.4.5", got.Package.Version)
	require.True(t, got.Package.Signed)
	require.Equal(t, "always", got.Package.Verification)
	require.NotEmpty(t, got.Package.Digest)

	require.Len(t, got.Phases, 1)
	require.Equal(t, "Syncing engine config", got.Phases[0].Title)
	require.Equal(t, "ran", got.Phases[0].Disposition)
	require.Equal(t, "4.2s", got.Phases[0].Duration)

	// The path and the reason survive; only the body of the diff goes.
	details := got.Phases[0].Hosts[0].Details
	require.Len(t, details, 1)
	require.Equal(t, "/etc/rancher/rke2/config.yaml", details[0].Path)
	require.Equal(t, "out of sync", details[0].Reason)
	require.Empty(t, details[0].Diff)

	// The shape of the fleet is what reproduces a bug.
	require.Len(t, got.Hosts, 2)
	require.Equal(t, "controller", got.Hosts[0].Role)
	require.Equal(t, "rocky", got.Hosts[0].OS)
	require.Equal(t, "arm64", got.Hosts[1].Arch)

	require.Equal(t, "partial", got.Outcome.ChangedSignal)
	require.Equal(t, []string{"Connect"}, got.Outcome.ChangedUndeclared)
}

// TestRedactPseudonymsAreStableAcrossPhases is what keeps the redacted receipt readable. A
// maintainer has to be able to follow one host through the ledger, so the alias a host gets in
// the host list has to be the alias it gets inside every phase.
func TestRedactPseudonymsAreStableAcrossPhases(t *testing.T) {
	in, err := Read(sensitiveFixture)
	require.NoError(t, err)

	got := Redact(in)

	require.Equal(t, "host-01", got.Hosts[0].Address)
	require.Equal(t, "host-02", got.Hosts[1].Address)

	// The first host in the configuration is the one the phase changed.
	require.Equal(t, "host-01", got.Phases[0].Hosts[0].Host)
	require.True(t, got.Phases[0].Hosts[0].Changed)
	require.Equal(t, "host-02", got.Phases[0].Hosts[1].Host)
	require.False(t, got.Phases[0].Hosts[1].Changed)
}

// TestRedactAliasesAnAddressOnlyAPhaseMentions pins the fallback. An address that appears in a
// phase but not in the host list would otherwise map to the empty string, silently merging two
// hosts into one in the report.
func TestRedactAliasesAnAddressOnlyAPhaseMentions(t *testing.T) {
	in := Receipt{
		Hosts: []Host{{Address: "10.0.0.1"}},
		Phases: []Phase{
			{
				Title:         "sync",
				HostsDeclared: true,
				Hosts: []HostChange{
					{
						Host: "10.0.0.1",
					},
					{
						Host: "10.0.0.99",
					},
				},
			},
		},
	}

	got := Redact(in)
	require.Equal(t, "host-01", got.Phases[0].Hosts[0].Host)
	require.Equal(t, "host-02", got.Phases[0].Hosts[1].Host)
	require.NotContains(t, got.Phases[0].Hosts[1].Host, "10.0.0.99")
}

// TestRedactIsIdempotentlyMarked pins that redacting twice does not renumber or re-alias, so a
// caller that cannot tell whether a file was already redacted is not punished for checking.
func TestRedactIsIdempotentlyMarked(t *testing.T) {
	in, err := Read(sensitiveFixture)
	require.NoError(t, err)

	once := Redact(in)
	twice := Redact(once)
	require.Equal(t, once, twice)
}

// TestRedactedFieldsAreClassified is the test that has to keep working as the types grow.
//
// Every field of Receipt and the structs it holds is either carried into the redacted copy or
// deliberately dropped. Adding one without deciding which is how a leak gets introduced months
// from now, so the field sets are pinned here and a new field fails this test until somebody
// classifies it.
func TestRedactedFieldsAreClassified(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "Receipt",
			got:  fieldNames[Receipt](),
			want: []string{"SchemaVersion", "Redacted", "Invocation", "Package", "Cluster", "Phases", "Hosts", "Outcome"},
		},
		{
			name: "Invocation",
			got:  fieldNames[Invocation](),
			want: []string{"Command", "Args", "Version", "Commit", "Operator", "Host", "Platform", "DryRun", "StartedAt", "FinishedAt", "Duration"},
		},
		{
			name: "Package",
			got:  fieldNames[Package](),
			want: []string{"Path", "Name", "Version", "SHA256", "Digest", "Architecture", "Engine", "Signed", "Verification"},
		},
		{
			name: "Cluster",
			got:  fieldNames[Cluster](),
			want: []string{"Name", "LoadBalancer", "ConfigPath", "HostCount"},
		},
		{
			name: "Phase",
			got:  fieldNames[Phase](),
			want: []string{"Title", "Explanation", "Disposition", "DryRun", "Declared", "Changed", "HostsDeclared", "Hosts", "Duration"},
		},
		{
			name: "HostChangeDetail",
			got:  fieldNames[HostChangeDetail](),
			want: []string{"Path", "Reason", "Diff"},
		},
		{
			name: "Outcome",
			got:  fieldNames[Outcome](),
			want: []string{"Succeeded", "Error", "Changed", "ChangedSignal", "ChangedUndeclared", "HostsUndeclared", "Warnings"},
		},
		{
			name: "Warning",
			got:  fieldNames[Warning](),
			want: []string{"Phase", "Host", "Path", "Reason"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.got,
				"a field changed on %s; decide whether Redact should carry or drop it, then update this list",
				tc.name)
		})
	}
}

// TestRedactedReceiptCarriesNoDiffAnywhere is a belt-and-braces sweep over the serialized
// document for the one field that can hold key material, rather than over the structs that are
// meant to hold it.
func TestRedactedReceiptCarriesNoDiffAnywhere(t *testing.T) {
	in, err := Read(sensitiveFixture)
	require.NoError(t, err)

	out, err := json.Marshal(Redact(in))
	require.NoError(t, err)

	require.NotContains(t, string(out), `"diff"`,
		"a redacted receipt must carry no diff field at all")
}
