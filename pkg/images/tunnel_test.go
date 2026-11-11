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

package images_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/pkg/images"
	ggcrname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	ggcr "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	ggcrremote "github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	ggcrtypes "github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	"golang.org/x/crypto/ssh"
	"oras.land/oras-go/v2"
	orasoci "oras.land/oras-go/v2/content/oci"
	orasRemote "oras.land/oras-go/v2/registry/remote"
)

// TestPushThroughTunnel proves that a push reaches a registry it has no route to, over an SSH
// direct-tcpip channel, with the destination references unchanged.
//
// This is the load-bearing question for mirroring into a registry that only a cluster node can
// reach: cargoship already holds an SSH connection to every host, and if the registry protocol
// travels over that channel then no Kubernetes port-forward, no API port and no node-side
// tooling are needed. What the test pins down is the part that is not obvious from reading the
// libraries -- that ORAS is indifferent to where the connection actually goes, so the reference
// a node will ask for and the address the bytes travel to can be decided separately.
//
// Everything here is in process: an SSH server that honours direct-tcpip, an in-memory
// registry, and an OCI layout standing in for an extracted package. The registry listens on
// loopback and the dialer is the only thing that knows its address, so a push that ignored the
// dialer would fail rather than quietly succeed by another route.
func TestPushThroughTunnel(t *testing.T) {
	ctx := context.Background()

	registryAddr := inMemoryRegistry(t)
	sshAddr, hostKey := tunnelSSHServer(t)

	// The one SSH connection, dialled the way a cargoship host connection is.
	client, err := ssh.Dial("tcp", sshAddr, &ssh.ClientConfig{
		User:            "cargoship",
		Auth:            []ssh.AuthMethod{ssh.Password("unused")},
		HostKeyCallback: ssh.FixedHostKey(hostKey),
		Timeout:         10 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() }) //nolint:errcheck

	// The dialer ignores the address it is handed and sends every connection down the channel
	// to the registry. That is exactly what a tunnel to a node does: the reference says
	// "the mirror", the bytes go through SSH.
	var dialled []string
	dialer := func(_ context.Context, network, addr string) (net.Conn, error) {
		dialled = append(dialled, addr)
		return client.Dial(network, registryAddr)
	}

	// A destination host that resolves nowhere. If the dialer were not used, the push could
	// not possibly succeed -- which is what makes this an assertion rather than a coincidence.
	const target = "mirror.invalid:5000"
	const imageRef = "registry.k8s.io/pause:3.10"

	layoutDir := ociLayoutWithImage(t, imageRef)
	img, err := transform.ParseImageRef(imageRef)
	require.NoError(t, err)

	// Without the dialer there is no route to mirror.invalid at all, which is what makes the
	// success below an assertion about the tunnel rather than about the network.
	err = images.Push(ctx, []transform.Image{img}, layoutDir, target, images.PushOptions{
		PlainHTTP: true,
		Retries:   1,
	})
	require.Error(t, err, "mirror.invalid resolved without a dialer; the tunnel is not what this test is measuring")

	// PlainHTTP is left false on purpose: the scheme probe ShouldUsePlainHTTP runs over the
	// same client, so this covers the negotiation travelling through the channel too, not only
	// the push that follows it.
	err = images.Push(ctx, []transform.Image{img}, layoutDir, target, images.PushOptions{
		Retries:     1,
		DialContext: dialer,
	})
	require.NoError(t, err, "the push did not survive the tunnel")

	require.NotEmpty(t, dialled, "the push bypassed the dialer, so this proves nothing")
	for _, addr := range dialled {
		require.Equal(t, target, addr,
			"ORAS dialled something other than the reference host, so the reference and the route are not separable after all")
	}

	// The image is in the registry under the reference a node would ask for: the host swapped,
	// the path and tag untouched. Read back over a direct connection, not the tunnel.
	ref, err := ggcrname.ParseReference(fmt.Sprintf("%s/pause:3.10", registryAddr))
	require.NoError(t, err)
	pushed, err := ggcrremote.Image(ref)
	require.NoError(t, err, "the tunnel carried the push but the registry does not hold the image")
	_, err = pushed.Manifest()
	require.NoError(t, err)
}

// tunnelSSHServer starts an SSH server that serves direct-tcpip channel requests by dialling the
// address the client asked for and splicing the two connections together. That is the whole of
// what `ssh -L` does on the server side, and it is what rig's Connection.Dial drives.
//
// It accepts any credentials: what is under test is the forwarding, not the authentication.
func tunnelSSHServer(t *testing.T) (addr string, hostKey ssh.PublicKey) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
			return nil, nil
		},
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

	return ln.Addr().String(), signer.PublicKey()
}

func serveSSHConn(nConn net.Conn, cfg *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(nConn, cfg)
	if err != nil {
		_ = nConn.Close() //nolint:errcheck
		return
	}
	defer conn.Close() //nolint:errcheck
	go ssh.DiscardRequests(reqs)

	for newChan := range chans {
		if newChan.ChannelType() != "direct-tcpip" {
			_ = newChan.Reject(ssh.UnknownChannelType, "only direct-tcpip is served") //nolint:errcheck
			continue
		}
		go forwardChannel(newChan)
	}
}

// forwardChannel reads the direct-tcpip request, dials the target it names, and copies in both
// directions until either side is done.
func forwardChannel(newChan ssh.NewChannel) {
	// The payload layout is RFC 4254 section 7.2.
	var req struct {
		DestAddr   string
		DestPort   uint32
		OriginAddr string
		OriginPort uint32
	}
	if err := ssh.Unmarshal(newChan.ExtraData(), &req); err != nil {
		_ = newChan.Reject(ssh.ConnectionFailed, "malformed direct-tcpip request") //nolint:errcheck
		return
	}

	upstream, err := net.Dial("tcp", net.JoinHostPort(req.DestAddr, fmt.Sprint(req.DestPort)))
	if err != nil {
		_ = newChan.Reject(ssh.ConnectionFailed, err.Error()) //nolint:errcheck
		return
	}

	ch, chReqs, err := newChan.Accept()
	if err != nil {
		_ = upstream.Close() //nolint:errcheck
		return
	}
	go ssh.DiscardRequests(chReqs)

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

// inMemoryRegistry starts a plain-HTTP registry on loopback. It mirrors test.SetupInMemoryRegistry,
// which this package cannot import without a dependency cycle through the e2e helpers.
func inMemoryRegistry(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := &http.Server{
		Handler:           registry.New(registry.Logger(log.New(io.Discard, "", 0))),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		_ = srv.Serve(ln) //nolint:errcheck
	}()
	// Close rather than Shutdown: the tunnel leaves keep-alive connections open, and a graceful
	// shutdown waits out its whole timeout for them.
	t.Cleanup(func() { _ = srv.Close() }) //nolint:errcheck

	return ln.Addr().String()
}

// ociLayoutWithImage writes a one-layer image into an OCI layout tagged under ref, which is the
// shape an extracted package's images directory has.
func ociLayoutWithImage(t *testing.T, ref string) string {
	t.Helper()
	ctx := context.Background()

	// Stage the image in a registry first, because go-containerregistry writes a manifest ORAS
	// can resolve and oras.Copy then lands it in the layout under the full reference as a tag.
	stagingAddr := inMemoryRegistry(t)

	layer := static.NewLayer([]byte("cargoship tunnel test"), ggcrtypes.DockerLayer)
	img, err := mutate.AppendLayers(empty.Image, layer)
	require.NoError(t, err)
	cfgFile, err := img.ConfigFile()
	require.NoError(t, err)
	cfgFile.Architecture = "amd64"
	cfgFile.OS = "linux"
	img, err = mutate.ConfigFile(img, cfgFile)
	require.NoError(t, err)

	stagedRef, err := ggcrname.ParseReference(stagingAddr + "/pause:3.10")
	require.NoError(t, err)
	require.NoError(t, ggcrremote.Write(stagedRef, img))

	var fetched ggcr.Image
	fetched, err = ggcrremote.Image(stagedRef)
	require.NoError(t, err)
	_, err = fetched.Manifest()
	require.NoError(t, err)

	layoutDir := t.TempDir()
	store, err := orasoci.New(layoutDir)
	require.NoError(t, err)

	repo, err := orasRemote.NewRepository(stagingAddr + "/pause:3.10")
	require.NoError(t, err)
	repo.PlainHTTP = true

	// Tagged under the upstream reference, which is the key images.Push resolves by.
	_, err = oras.Copy(ctx, repo, "3.10", store, ref, oras.DefaultCopyOptions)
	require.NoError(t, err)

	return layoutDir
}
