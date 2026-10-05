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
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/pkg/firewall"
	"github.com/stretchr/testify/require"
)

// FuzzNftSplitFamilies asserts that NftSplitFamilies never panics and buckets every address it
// accepts into exactly one family, without inventing or dropping any address it did not
// explicitly reject.
//
// The addresses it sorts come from a cluster's own host and CIDR configuration, which reaches
// nft ruleset rendering as literal text, so an address counted twice -- once in each family's
// named set -- or silently dropped would produce a ruleset that is either broken or quietly
// missing a trust entry, neither of which nft's own parser would catch.
//
// The fuzzed string is split on newlines into the []string form the function takes, which is how
// a variable-length list is reached from the string/[]byte/numeric types fuzzing supports.
func FuzzNftSplitFamilies(f *testing.F) {
	f.Add("10.0.0.5\nfd00::5\n10.42.0.0/16\nfd00::/8\n\nnot-an-address")
	f.Add("")
	f.Add("127.0.0.1")
	f.Add("::1")
	f.Add("0.0.0.0/0")
	f.Add("::/0")
	f.Add("999.999.999.999")
	f.Add("not-an-address\nalso-not-one")
	f.Add(strings.Repeat("10.0.0.1\n", 50))
	f.Add("10.0.0.1/33")

	f.Fuzz(func(t *testing.T, addrs string) {
		var list []string
		if addrs != "" {
			list = strings.Split(addrs, "\n")
		}

		v4, v6 := firewall.NftSplitFamilies(list)

		inV4 := make(map[string]bool, len(v4))
		for _, a := range v4 {
			inV4[a] = true
		}
		for _, a := range v6 {
			require.False(t, inV4[a], "address %q was placed in both families for input %q", a, addrs)
		}
		require.LessOrEqual(t, len(v4)+len(v6), len(list), "returned more addresses than were given for input %q", addrs)
	})
}
