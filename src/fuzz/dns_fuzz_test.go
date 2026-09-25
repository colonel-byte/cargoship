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

package fuzz

import (
	"testing"

	"github.com/colonel-byte/cargoship/src/internal/dns"
	"github.com/stretchr/testify/require"
)

// FuzzParseServiceURL asserts that ParseServiceURL either fails or returns a namespace, service
// name and port that are internally consistent with the input, and that IsServiceURL always agrees
// with whether ParseServiceURL succeeded.
//
// This is the parser that turns a registry or engine URL an operator or package author wrote into
// the in-cluster service cargoship connects to instead, so the property worth fuzzing is not just
// "does not panic" but that a name and namespace it hands back are ones that were actually present
// in the string given, not artifacts of the regex matching something it should not have.
func FuzzParseServiceURL(f *testing.F) {
	f.Add("http://foo.bar.svc.cluster.local:5000")
	f.Add("https://registry.zarf.svc.cluster.local:443")
	f.Add("http://foo.bar.svc.cluster.local")
	f.Add("foo.bar.svc.cluster.local:5000")
	f.Add("http://foo.bar.baz.svc.cluster.local:5000")
	f.Add("http://.svc.cluster.local:5000")
	f.Add("http://foo.svc.cluster.local:99999")
	f.Add("http://foo.bar.svc.cluster.local:-1")
	f.Add("")
	f.Add("not a url at all")
	f.Add("http://[::1]:5000")
	f.Add("http://foo.bar.svc.cluster.local:5000/some/path?x=1")

	f.Fuzz(func(t *testing.T, serviceURL string) {
		namespace, name, port, err := dns.ParseServiceURL(serviceURL)

		require.Equal(t, err == nil, dns.IsServiceURL(serviceURL),
			"IsServiceURL disagrees with ParseServiceURL on %q", serviceURL)

		if err != nil {
			return
		}
		require.Positive(t, port, "accepted a non-positive port from %q", serviceURL)
		require.NotEmpty(t, namespace, "accepted an empty namespace from %q", serviceURL)
		require.NotEmpty(t, name, "accepted an empty service name from %q", serviceURL)
		require.NotContains(t, namespace, ".", "namespace %q is not a single DNS label", namespace)
		require.NotContains(t, name, ".", "service name %q is not a single DNS label", name)
		require.Contains(t, serviceURL, name+"."+namespace+".svc.cluster.local",
			"returned name %q and namespace %q are not present as such in %q", name, namespace, serviceURL)
	})
}

// FuzzIsLocalhostNoPanic asserts that IsLocalhost never panics, whatever string it is asked to
// classify.
//
// IsLocalhost decides whether a registry cargoship is about to pull from or push to counts as
// local, which gates whether TLS is required. isRFC1918 underneath it parses the input as a bare
// host and matches it against fixed CIDRs, so a colon-heavy or otherwise malformed URL is exactly
// the input worth throwing at it: net.ParseIP and net.ParseCIDR are both in that path and neither
// promises much about strings that are not what they expect.
func FuzzIsLocalhostNoPanic(f *testing.F) {
	f.Add("localhost:5000")
	f.Add("127.0.0.1:5000")
	f.Add("10.0.0.1")
	f.Add("192.168.1.1:443")
	f.Add("[::1]:5000")
	f.Add("example.com")
	f.Add("example.local")
	f.Add("")
	f.Add(":::::")
	f.Add("999.999.999.999")
	f.Add("10.0.0.1:not-a-port")

	f.Fuzz(func(_ *testing.T, url string) {
		_ = dns.IsLocalhost(url)
	})
}
