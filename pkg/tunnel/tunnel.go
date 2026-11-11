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

// Package tunnel reaches a service inside a cluster over an SSH connection to one of its nodes.
//
// It exists because the services cargoship wants to talk to are frequently not addressable from
// the machine cargoship runs on. A registry a Zarf deployment stands up records a NodePort or a
// per-node host port -- an address only a node can reach -- and docs/agent/choice-removed-hosts.md
// records the repository's position that a cluster whose firewall admits only SSH from outside is
// an ordinary cargoship deployment. The SSH connection is therefore the one route that can be
// relied on, and an SSH direct-tcpip channel turns it into a route for arbitrary TCP.
//
// What this deliberately is not is a Kubernetes port-forward. That would need
// k8s.io/client-go/tools/portforward, which imports k8s.io/api/core/v1 unconditionally and so
// relinks the module docs/agent/choice-dynamic-kube-client.md exists to keep out, plus three
// modules absent from go.sum. It would also need the API port open, which is the problem rather
// than the solution.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/k0sproject/rig/v2/protocol/ssh"
)

// ErrNoSSH marks a host that cannot carry a tunnel. It is a distinct error because the cause is
// always the inventory rather than the network, and the remedy is to configure the host with an
// `ssh:` block.
var ErrNoSSH = errors.New("host has no ssh configuration to tunnel through")

// Dialer opens connections to one service address inside a cluster, carrying each over an SSH
// channel to a node.
//
// A Dialer is bound to a single service address at construction, and DialContext sends every
// connection there regardless of the address it is asked for. That redirection is the whole
// point rather than a quirk: it is what lets the reference an image is pushed under -- the one
// a node will later ask a mirror for -- be decided separately from the route the bytes take.
type Dialer struct {
	conn    *ssh.Connection
	service string
}

// To opens an SSH connection described by cfg and returns a Dialer that reaches service through
// it. service is an address as the node sees it, so "127.0.0.1:31999" for a NodePort.
//
// The connection is a second one to the host rather than the live one a phase is already holding:
// rig keeps its protocol connection unexported with no accessor (rig/v2/client.go:55), so there
// is nothing to borrow. Close releases it.
func To(ctx context.Context, cfg *ssh.Config, service string) (*Dialer, error) {
	if cfg == nil {
		return nil, ErrNoSSH
	}
	if service == "" {
		return nil, errors.New("a service address to tunnel to is required")
	}

	// A copy, because Connect mutates the config it is handed -- SetDefaults fills the port and
	// the user -- and the caller's inventory should not acquire our defaults as a side effect.
	local := *cfg
	conn, err := ssh.NewConnection(local)
	if err != nil {
		return nil, fmt.Errorf("unable to prepare an ssh connection to %s: %w", cfg.Address, err)
	}
	if err := conn.Connect(ctx); err != nil {
		return nil, fmt.Errorf("unable to connect to %s to tunnel through it: %w", cfg.Address, err)
	}

	return &Dialer{conn: conn, service: service}, nil
}

// DialContext opens one connection to the Dialer's service address over the SSH channel.
//
// The address it is handed is ignored, by design -- see Dialer. The signature is the one
// http.Transport.DialContext takes, which is how this reaches an OCI client: see
// images.ClientOptions.DialContext.
func (d *Dialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	// rig's Dial takes no context. The check keeps a cancelled push from opening another
	// channel, which is as much as can be honoured without reimplementing the dial.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	conn, err := d.conn.Dial(network, d.service)
	if err != nil {
		return nil, fmt.Errorf("unable to reach %s through %s: %w", d.service, d.conn.Address, err)
	}
	return conn, nil
}

// Service is the address inside the cluster this Dialer reaches, for logging.
func (d *Dialer) Service() string {
	return d.service
}

// Close releases the SSH connection. Connections already opened through it are closed with it.
func (d *Dialer) Close() {
	d.conn.Disconnect()
}
