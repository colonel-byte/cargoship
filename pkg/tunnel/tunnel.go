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
// What this deliberately is not is a Kubernetes port-forward, and the reason is the API port
// rather than the dependency. A port-forward needs the API server reachable, which on a fleet
// whose firewall admits port 22 and nothing else is the problem rather than the solution.
//
// The dependency half of that argument no longer holds, and pkg/kubetunnel is the port-forward
// this paragraph used to rule out: k8s.io/client-go/tools/portforward does cost 3.34MiB for its
// import of k8s.io/api/core/v1, but hand-writing the channel protocol over
// client-go/transport/websocket costs 132KiB and links no API group. The two packages are both
// here because they answer different situations -- this one is the only route into an air-gapped
// fleet, and kubetunnel is the only route into a cluster whose nodes have no sshd at all, which
// is every k3d, kind and Talos cluster. See docs/agent/choice-dynamic-kube-client.md for the
// measurements.
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

// Conn is an SSH connection to a node, able to reach any number of services inside the cluster
// through it. One connection serves them all: a mirror reaches both the Kubernetes API, to read
// where the registry is, and the registry itself, without authenticating twice.
type Conn struct {
	conn *ssh.Connection
}

// Open connects to the host cfg describes.
//
// The connection is a second one to the host rather than the live one a phase may already be
// holding: rig keeps its protocol connection unexported with no accessor (rig/v2/client.go:55),
// so there is nothing to borrow. Close releases it.
func Open(ctx context.Context, cfg *ssh.Config) (*Conn, error) {
	if cfg == nil {
		return nil, ErrNoSSH
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

	return &Conn{conn: conn}, nil
}

// Address is the host this connection reaches, for logging.
func (c *Conn) Address() string {
	return c.conn.Address
}

// To returns a Dialer that reaches service over this connection. service is an address as the
// node sees it, so "127.0.0.1:31999" for a NodePort.
func (c *Conn) To(service string) (*Dialer, error) {
	if service == "" {
		return nil, errors.New("a service address to tunnel to is required")
	}
	return &Dialer{conn: c.conn, service: service}, nil
}

// Dialer opens connections to one service address inside a cluster, carrying each over an SSH
// channel to a node.
//
// A Dialer is bound to a single service address, and DialContext sends every connection there
// regardless of the address it is asked for. That redirection is the whole point rather than a
// quirk: it is what lets a name be decided separately from the route taken to reach it -- the
// reference an image is pushed under, or the hostname a server certificate is validated against.
type Dialer struct {
	conn    *ssh.Connection
	service string
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

// Close releases the SSH connection, and with it every connection opened through any Dialer it
// handed out.
func (c *Conn) Close() {
	c.conn.Disconnect()
}
