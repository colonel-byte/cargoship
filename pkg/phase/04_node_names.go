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

package phase

import (
	"context"
	"strings"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// Every phase that drives kubectl against one host's node needs the name that node carries in
// the cluster, and no phase can guess it: the delete phases learned that the hard way, and the
// upgrade and config-sync phases drained a node by the name the machine reports until they were
// brought here too. The resolution is shared so the next phase to need it does not learn it a
// third time.

const (
	// listNodeIdentities prints one line per node: its name, then the addresses it reports as
	// one comma-separated field. That is what a host is matched against, because the name
	// alone is not enough -- see resolveNodeNames.
	//
	// custom-columns rather than jsonpath. The command is built as a shell string and run
	// through an SSH session, and a jsonpath template carrying the double quotes and backslash
	// of {"\n"} does not survive that: the shell strips the quotes and kubectl rejects what is
	// left, which showed up as every host failing to match any node. This form has nothing in
	// it a shell will touch.
	listNodeIdentities = `get nodes --no-headers ` +
		`-o custom-columns=:.metadata.name,:.status.addresses[*].address 2>/dev/null`
)

// resolveNodeNames maps each host to the name its node carries in the cluster, for the hosts
// that have one.
//
// A host cannot be matched to its node by one guessed name. The node's name comes from the
// engine config's `node-name`, which cargoship writes from the inventory's hostname, while
// `h.Configurer.Hostname(h)` is what the machine reports -- and on any host with a search
// domain those differ: the machine says `kw1.cargoship.test` and the node is `kw1`. Probing the
// reported name returned "not found" for a node that was there, the host was dropped from the
// phase, and the engine was uninstalled by a later phase that does no such probe. The node
// stayed in the cluster with nothing left to remove it.
//
// So the nodes are listed once and matched on any identifier the host and the node share,
// which is the same notion of "this node is this host" that DetectRemovedHosts already uses on
// the apply side. One kubectl call for the whole phase rather than one per host.
//
// A host with no matching node is absent from the result, which is how the phases tell apart
// "this host is in the cluster" from "this host is not", without a second probe.
func resolveNodeNames(ctx context.Context, leader *cluster.Host, d distrocfg.Distro, hosts cluster.Hosts) map[string]string {
	if leader == nil || len(hosts) == 0 {
		return nil
	}

	out, err := leader.Sudo().ExecOutput(
		d.KubectlCmdf(leader, d.DataDirPath(), listNodeIdentities))
	if err != nil {
		logger.From(ctx).Warn("could not list the cluster's nodes, so no host can be matched to one", "error", err)
		return nil
	}
	return matchNodesToHosts(out, hosts)
}

// matchNodesToHosts pairs the lines listNodeIdentities printed with the hosts they belong to.
//
// Split out from resolveNodeNames so the matching is testable without a cluster: the parsing
// and the identifier comparison are where this goes wrong, not the kubectl call.
func matchNodesToHosts(listed string, hosts cluster.Hosts) map[string]string {
	byHost := make(map[string]string, len(hosts))

	for line := range strings.SplitSeq(listed, "\n") {
		// Whitespace separates the name from the address column; commas separate the addresses
		// within it. Splitting on both leaves one identity per element whichever column it
		// came from, and the node's own name is the first.
		fields := strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t'
		})
		if len(fields) == 0 {
			continue
		}
		node := fields[0]

		identities := make(map[string]struct{}, len(fields))
		for _, f := range fields {
			identities[strings.ToLower(f)] = struct{}{}
		}

		for _, h := range hosts {
			if _, taken := byHost[h.String()]; taken {
				continue
			}
			if hostMatchesIdentities(h, identities) {
				byHost[h.String()] = node
			}
		}
	}
	return byHost
}

// hostMatchesIdentities reports whether any name or address the host is known by appears among
// the identities a node reported. The set is the same one DetectRemovedHosts builds.
func hostMatchesIdentities(h *cluster.Host, identities map[string]struct{}) bool {
	for _, id := range []string{h.Metadata.Hostname, h.Hostname, h.PrivateAddress, h.Address()} {
		if id == "" {
			continue
		}
		if _, ok := identities[strings.ToLower(id)]; ok {
			return true
		}
	}
	return false
}

// nodeNameFor is the name h's node carries in the cluster, falling back to the name the
// machine reports when the nodes could not be listed at all. The fallback keeps the old
// behaviour for a cluster that cannot be read rather than silently acting on an empty name.
func nodeNameFor(names map[string]string, h *cluster.Host) string {
	if name, ok := names[h.String()]; ok {
		return name
	}
	return h.Configurer.Hostname(h)
}
