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

package tunnel_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/pkg/tunnel"
	rigssh "github.com/k0sproject/rig/v2/protocol/ssh"
	"github.com/stretchr/testify/require"
	cryptossh "golang.org/x/crypto/ssh"
)

// TestToReachesAServiceOnlyTheNodeCanSee drives rig's own SSH connection, not crypto/ssh
// directly, so what is proven is the wiring cargoship would actually use.
//
// The HTTP server stands in for an in-cluster registry: it listens on loopback and the tunnel is
// the only thing told its address, so a request that failed to travel through the channel could
// not reach it.
func TestToReachesAServiceOnlyTheNodeCanSee(t *testing.T) {
	ctx := context.Background()

	service := echoServer(t)
	sshAddr := tunnelSSHServer(t)

	conn, err := tunnel.Open(ctx, sshConfigFor(t, sshAddr))
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	d, err := conn.To(service)
	require.NoError(t, err)
	require.Equal(t, service, d.Service())

	// An http.Client is the consumer this exists for, so it is what the test uses.
	client := &http.Client{Transport: &http.Transport{DialContext: d.DialContext}}

	// A URL naming a host that resolves nowhere. Reaching the service anyway is the assertion:
	// the reference and the route are separate, which is what mirroring needs.
	resp, err := client.Get("http://mirror.invalid:5000/ping") //nolint:noctx
	require.NoError(t, err, "the request did not survive the tunnel")
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "pong /ping", string(body))
}

// TestToRefusesAHostItCannotTunnelThrough covers the inventory error. A host configured with
// `openSSH:` rather than `ssh:` has no config here, and rig's openssh protocol implements no
// Dial, so there is nothing to fall back to -- which makes saying so the whole remedy.
func TestOpenRefusesAHostItCannotTunnelThrough(t *testing.T) {
	_, err := tunnel.Open(context.Background(), nil)
	require.ErrorIs(t, err, tunnel.ErrNoSSH)
}

func TestToRequiresAServiceAddress(t *testing.T) {
	sshAddr := tunnelSSHServer(t)
	conn, err := tunnel.Open(context.Background(), sshConfigFor(t, sshAddr))
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	_, err = conn.To("")
	require.ErrorContains(t, err, "service address")
}

// TestOneConnectionServesSeveralServices is what the mirror depends on: the Kubernetes API is
// read through the same connection the push then travels over, so a fleet is authenticated to
// once rather than twice.
func TestOneConnectionServesSeveralServices(t *testing.T) {
	first := echoServer(t)
	second := echoServer(t)

	sshAddr := tunnelSSHServer(t)
	conn, err := tunnel.Open(context.Background(), sshConfigFor(t, sshAddr))
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	for _, service := range []string{first, second} {
		d, err := conn.To(service)
		require.NoError(t, err)

		client := &http.Client{Transport: &http.Transport{DialContext: d.DialContext}}
		resp, err := client.Get("http://whatever.invalid/" + service) //nolint:noctx
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, resp.Body.Close())
		require.NoError(t, err)
		require.Equal(t, "pong /"+service, string(body))
	}
}

// TestDialContextHonoursACancelledContext pins the one thing DialContext can do about
// cancellation, since rig's Dial takes no context.
func TestDialContextHonoursACancelledContext(t *testing.T) {
	service := echoServer(t)
	sshAddr := tunnelSSHServer(t)

	conn, err := tunnel.Open(context.Background(), sshConfigFor(t, sshAddr))
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	d, err := conn.To(service)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = d.DialContext(ctx, "tcp", "mirror.invalid:5000")
	require.ErrorIs(t, err, context.Canceled)
}

// sshConfigFor points rig at the in-process server: in-memory credentials, host key checking
// off because the key is generated per run, and the OpenSSH client configuration ignored so a
// developer's own ~/.ssh/config cannot change the result.
func sshConfigFor(t *testing.T, addr string) *rigssh.Config {
	t.Helper()

	host, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	p, err := strconv.Atoi(port)
	require.NoError(t, err)

	return &rigssh.Config{
		Address:         host,
		Port:            p,
		User:            "cargoship",
		AuthMethods:     []cryptossh.AuthMethod{cryptossh.Password("unused")},
		IgnoreSSHConfig: true,
		SSHConfigOptions: map[string]any{
			"StrictHostKeyChecking": false,
			"UserKnownHostsFile":    "/dev/null",
		},
	}
}

// echoServer starts an HTTP server on loopback that answers with the path it was asked for, so a
// response proves the request arrived intact rather than merely that a socket opened.
func echoServer(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprintf(w, "pong %s", r.URL.Path)
		}),
	}
	go func() {
		_ = srv.Serve(ln) //nolint:errcheck
	}()
	// Close rather than Shutdown: the tunnel leaves keep-alive connections open, and a graceful
	// shutdown waits out its whole timeout for them.
	t.Cleanup(func() { _ = srv.Close() }) //nolint:errcheck

	return ln.Addr().String()
}

// tunnelSSHServer starts an SSH server that serves direct-tcpip channel requests by dialling the
// address the client asked for and splicing the two connections together. That is what `ssh -L`
// does on the server side, and what rig's Connection.Dial drives.
//
// It accepts any credentials: what is under test is the forwarding, not the authentication.
func tunnelSSHServer(t *testing.T) (addr string) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := cryptossh.NewSignerFromKey(priv)
	require.NoError(t, err)

	cfg := &cryptossh.ServerConfig{
		PasswordCallback: func(cryptossh.ConnMetadata, []byte) (*cryptossh.Permissions, error) {
			return nil, nil
		},
		// rig probes for Windows on connect, and falls back to running ver.exe over a session
		// channel when it does not recognise the server version. Naming a POSIX one short
		// circuits that (protocol/ssh/knownposix.go), so this server need serve only the
		// direct-tcpip channels the tunnel is about. Without it every connect waits out the
		// 10-second probe timeout in prewarmWindows.
		ServerVersion: "SSH-2.0-OpenSSH_9.6 linux",
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck

	go func() {
		for {
			nConn, err := ln.Accept()
			if err != nil {
				return // the listener closed with the test
			}
			go serveSSHConn(nConn, cfg)
		}
	}()

	return ln.Addr().String()
}

func serveSSHConn(nConn net.Conn, cfg *cryptossh.ServerConfig) {
	conn, chans, reqs, err := cryptossh.NewServerConn(nConn, cfg)
	if err != nil {
		_ = nConn.Close() //nolint:errcheck
		return
	}
	defer conn.Close() //nolint:errcheck
	go cryptossh.DiscardRequests(reqs)

	for newChan := range chans {
		if newChan.ChannelType() != "direct-tcpip" {
			_ = newChan.Reject(cryptossh.UnknownChannelType, "only direct-tcpip is served") //nolint:errcheck
			continue
		}
		go forwardChannel(newChan)
	}
}

// forwardChannel reads the direct-tcpip request, dials the target it names, and copies in both
// directions until either side is done.
func forwardChannel(newChan cryptossh.NewChannel) {
	// The payload layout is RFC 4254 section 7.2.
	var req struct {
		DestAddr   string
		DestPort   uint32
		OriginAddr string
		OriginPort uint32
	}
	if err := cryptossh.Unmarshal(newChan.ExtraData(), &req); err != nil {
		_ = newChan.Reject(cryptossh.ConnectionFailed, "malformed direct-tcpip request") //nolint:errcheck
		return
	}

	upstream, err := net.Dial("tcp", net.JoinHostPort(req.DestAddr, fmt.Sprint(req.DestPort)))
	if err != nil {
		_ = newChan.Reject(cryptossh.ConnectionFailed, err.Error()) //nolint:errcheck
		return
	}

	ch, chReqs, err := newChan.Accept()
	if err != nil {
		_ = upstream.Close() //nolint:errcheck
		return
	}
	go cryptossh.DiscardRequests(chReqs)

	go func() {
		defer ch.Close()             //nolint:errcheck
		defer upstream.Close()       //nolint:errcheck
		_, _ = io.Copy(ch, upstream) //nolint:errcheck
	}()
	go func() {
		defer ch.Close()             //nolint:errcheck
		defer upstream.Close()       //nolint:errcheck
		_, _ = io.Copy(upstream, ch) //nolint:errcheck
	}()
}
