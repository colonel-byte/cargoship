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

// Package kubetunnel reaches a service inside a cluster through the Kubernetes API server's
// port-forward subresource.
//
// It is the second route to the same place as pkg/tunnel, and the two are not alternatives so
// much as answers to different situations. pkg/tunnel carries a connection over SSH to a node,
// which is the only route a fleet whose firewall admits SSH and nothing else offers, and it is
// what a cargoship deployment uses. This package needs no SSH and no node address, only a
// kubeconfig -- which is the situation a development cluster puts an operator in. k3d and kind
// nodes are containers with no sshd, and Talos runs no shell at all, so a package mirrored into
// a registry on one of those clusters cannot travel the SSH route at all.
//
// It speaks the channel protocol over a websocket rather than SPDY. SPDY is deprecated upstream
// (KEP-4006), and the channel protocol needs no stream multiplexer: a frame's first byte names
// its channel, which is the whole framing. Hand-writing that is what keeps this cheap, and the
// cost is the reason -- see docs/agent/choice-dynamic-kube-client.md, which measures
// k8s.io/client-go/tools/portforward at 3.34MiB of binary for its import of k8s.io/api/core/v1,
// against 132KiB for the websocket transport this uses. Neither links an API group.
//
// A Dialer rather than a listener, deliberately. A local listener would mean every reference
// pointing at 127.0.0.1, which both loses the reference a node will ask for and breaks the
// server certificate's hostname -- the registry a Zarf deployment stands up serves TLS for its
// in-cluster name. Dialing instead leaves the reference alone and only redirects the bytes, the
// same trade pkg/tunnel.Dialer makes and for the same reason.
package kubetunnel

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	gwebsocket "github.com/gorilla/websocket"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport/websocket"
)

// channelProtocol is the websocket subprotocol the port-forward subresource serves alongside
// SPDY. v5 adds a close signal that only exec and attach use, so v4 is what a port-forward
// negotiates.
const channelProtocol = "v4.channel.k8s.io"

// A port is allotted two channels, data then error, in the order the ports were requested. One
// port is requested here, so the numbers are fixed.
const (
	dataChannel  byte = 0
	errorChannel byte = 1
)

// Dialer opens connections to one port on one pod, carrying each over its own websocket to the
// API server.
//
// Like pkg/tunnel.Dialer it is bound to a single destination and ignores the address it is asked
// for. See the package comment for why that redirection is the point.
type Dialer struct {
	cfg     *rest.Config
	url     *url.URL
	service string
}

// To resolves a service to one of its ready pods and returns a Dialer onto the port backing
// servicePort.
//
// One pod is picked, not all of them. A tunnel is not a load balancer, and a registry behind a
// service of several replicas is one where any replica can answer -- its blobs are in shared
// storage, or it is not a registry.
func To(ctx context.Context, cfg *rest.Config, namespace, service string, servicePort int) (*Dialer, error) {
	if cfg == nil {
		return nil, errors.New("a kubeconfig is required to tunnel through the api server")
	}
	pod, targetPort, err := resolveService(ctx, cfg, namespace, service, servicePort)
	if err != nil {
		return nil, err
	}
	u, err := portForwardURL(cfg.Host, cfg.APIPath, namespace, pod, targetPort)
	if err != nil {
		return nil, err
	}
	return &Dialer{
		cfg:     cfg,
		url:     u,
		service: fmt.Sprintf("%s/%s:%d", namespace, service, servicePort),
	}, nil
}

// Service is the service this Dialer reaches, for logging.
func (d *Dialer) Service() string {
	return d.service
}

// DialContext opens one connection to the Dialer's destination.
//
// The address it is handed is ignored, by design -- see Dialer. The signature is the one
// http.Transport.DialContext takes, which is how this reaches an OCI client: see
// images.ClientOptions.DialContext.
func (d *Dialer) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	ws, err := dial(ctx, d.cfg, d.url)
	if err != nil {
		return nil, err
	}
	return &conn{ws: ws, service: d.service}, nil
}

// portForwardURL builds the port-forward subresource URL for a pod.
func portForwardURL(host, apiPath, namespace, pod string, remotePort int) (*url.URL, error) {
	if namespace == "" || pod == "" {
		return nil, errors.New("a namespace and a pod name are required")
	}
	if remotePort < 1 || remotePort > 65535 {
		return nil, fmt.Errorf("port %d is out of range", remotePort)
	}
	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("unable to read the apiserver address %q: %w", host, err)
	}
	if u.Scheme == "" {
		return nil, fmt.Errorf("the apiserver address %q names no scheme", host)
	}
	if apiPath == "" {
		apiPath = "/api"
	}
	u = u.JoinPath(apiPath, "v1", "namespaces", namespace, "pods", pod, "portforward")
	u.RawQuery = url.Values{"ports": []string{strconv.Itoa(remotePort)}}.Encode()
	return u, nil
}

// dial opens one websocket to the port-forward subresource.
func dial(ctx context.Context, cfg *rest.Config, u *url.URL) (*gwebsocket.Conn, error) {
	rt, holder, err := websocket.RoundTripperFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to build a websocket transport from the kubeconfig: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	ws, err := websocket.Negotiate(rt, holder, req, channelProtocol)
	if err != nil {
		return nil, fmt.Errorf("unable to open a port-forward to %s: %w", u.Path, err)
	}
	if err := readPortAnnouncements(ws); err != nil {
		//nolint:errcheck // the connection is being abandoned because of err, which is what the caller needs
		ws.Close()
		return nil, err
	}
	return ws, nil
}

// readPortAnnouncements consumes the frame the server opens each channel with, which carries the
// port number as a little-endian uint16.
//
// Leaving these unread prepends two bytes of port number to the stream, which on an HTTP
// connection corrupts the first response line and is thoroughly confusing to debug.
func readPortAnnouncements(ws *gwebsocket.Conn) error {
	for range 2 {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			return fmt.Errorf("unable to read the port announcement: %w", err)
		}
		if len(msg) != 3 {
			return fmt.Errorf("expected a 3-byte port announcement, got %d bytes", len(msg))
		}
		if ch := msg[0]; ch != dataChannel && ch != errorChannel {
			return fmt.Errorf("port announced on unexpected channel %d", ch)
		}
		_ = binary.LittleEndian.Uint16(msg[1:])
	}
	return nil
}

// conn presents one websocket as a net.Conn, so that an http.Transport can use it without
// knowing it is not a socket.
type conn struct {
	ws      *gwebsocket.Conn
	service string

	// rest holds what a frame carried beyond what the last Read asked for. A websocket is
	// message-framed and a net.Conn is a stream, so a reader taking less than a whole frame
	// has to be able to come back for the remainder.
	rest []byte

	// remoteErr accumulates the error channel, which is where the API server reports a refused
	// connection on the far side. Returning it in place of the EOF that follows is what turns
	// "the registry is not listening" into something other than an unexplained short read.
	remoteErr strings.Builder

	// closeOnce guards the websocket against the double Close an http.Transport performs when
	// it retires an idle connection it has already failed a request on.
	closeOnce sync.Once
}

func (c *conn) Read(p []byte) (int, error) {
	for len(c.rest) == 0 {
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			if c.remoteErr.Len() > 0 {
				return 0, fmt.Errorf("%s reported: %s", c.service, c.remoteErr.String())
			}
			if isClosed(err) {
				return 0, io.EOF
			}
			return 0, err
		}
		if len(msg) == 0 {
			continue
		}
		switch msg[0] {
		case dataChannel:
			c.rest = msg[1:]
		case errorChannel:
			c.remoteErr.Write(msg[1:])
		}
	}

	n := copy(p, c.rest)
	c.rest = c.rest[n:]
	return n, nil
}

func (c *conn) Write(p []byte) (int, error) {
	frame := make([]byte, 0, len(p)+1)
	frame = append(frame, dataChannel)
	frame = append(frame, p...)
	if err := c.ws.WriteMessage(gwebsocket.BinaryMessage, frame); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *conn) Close() error {
	var err error
	c.closeOnce.Do(func() { err = c.ws.Close() })
	if isClosed(err) {
		return nil
	}
	return err
}

// LocalAddr and RemoteAddr report the websocket's own endpoints rather than the tunnelled
// service, because a caller using them is logging or tracing the connection it actually holds.
func (c *conn) LocalAddr() net.Addr  { return c.ws.LocalAddr() }
func (c *conn) RemoteAddr() net.Addr { return c.ws.RemoteAddr() }

func (c *conn) SetDeadline(t time.Time) error {
	if err := c.ws.SetReadDeadline(t); err != nil {
		return err
	}
	return c.ws.SetWriteDeadline(t)
}

func (c *conn) SetReadDeadline(t time.Time) error  { return c.ws.SetReadDeadline(t) }
func (c *conn) SetWriteDeadline(t time.Time) error { return c.ws.SetWriteDeadline(t) }

// resolveService reads the service's endpoints and returns a ready pod and the port on it that
// backs servicePort.
//
// The lookup goes over a raw REST request decoded into the few fields it needs, rather than
// through a typed clientset, for the reason docs/agent/choice-dynamic-kube-client.md records: a
// typed client registers every API group and costs megabytes for a handful of fields.
func resolveService(ctx context.Context, cfg *rest.Config, namespace, service string, servicePort int) (string, int, error) {
	if namespace == "" || service == "" {
		return "", 0, errors.New("a namespace and a service name are required")
	}

	client, err := rest.UnversionedRESTClientFor(restConfigForCore(cfg))
	if err != nil {
		return "", 0, fmt.Errorf("unable to build a REST client from the kubeconfig: %w", err)
	}

	raw, err := client.Get().AbsPath("/api/v1/namespaces", namespace, "endpoints", service).DoRaw(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("unable to read the endpoints of service %s/%s: %w", namespace, service, err)
	}
	return podBackingPort(raw, namespace, service, servicePort)
}

// podBackingPort picks a ready pod and its target port out of an Endpoints object.
func podBackingPort(raw []byte, namespace, service string, servicePort int) (string, int, error) {
	// Only the fields used are declared, so that an unrelated change in the Endpoints schema
	// cannot break the decode.
	var endpoints struct {
		Subsets []struct {
			Addresses []struct {
				TargetRef *struct {
					Kind string `json:"kind"`
					Name string `json:"name"`
				} `json:"targetRef"`
			} `json:"addresses"`
			Ports []struct {
				Port int `json:"port"`
			} `json:"ports"`
		} `json:"subsets"`
	}
	if err := json.Unmarshal(raw, &endpoints); err != nil {
		return "", 0, fmt.Errorf("unable to read the endpoints of service %s/%s: %w", namespace, service, err)
	}

	for _, subset := range endpoints.Subsets {
		// A subset's ports are the target ports its addresses serve. One entry is the
		// common case; where there are several the service port cannot pick between them,
		// so the first is taken.
		if len(subset.Ports) == 0 {
			continue
		}
		for _, addr := range subset.Addresses {
			if addr.TargetRef == nil || addr.TargetRef.Kind != "Pod" || addr.TargetRef.Name == "" {
				continue
			}
			return addr.TargetRef.Name, subset.Ports[0].Port, nil
		}
	}

	return "", 0, fmt.Errorf("service %s/%s has no ready pod backing port %d", namespace, service, servicePort)
}

// restConfigForCore fills in the group, version and codec a bare kubeconfig leaves unset, which
// rest.UnversionedRESTClientFor requires even for a request whose body is never decoded.
func restConfigForCore(cfg *rest.Config) *rest.Config {
	out := rest.CopyConfig(cfg)
	out.GroupVersion = &schema.GroupVersion{Group: "", Version: "v1"}
	out.APIPath = "/api"
	out.NegotiatedSerializer = runtime.NewSimpleNegotiatedSerializer(runtime.SerializerInfo{
		MediaType:        "application/json",
		MediaTypeType:    "application",
		MediaTypeSubType: "json",
	})
	return out
}

// isClosed reports whether err is one of the several ways a stream ending looks, none of which
// are faults worth reporting.
func isClosed(err error) bool {
	return err == nil ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) ||
		gwebsocket.IsCloseError(err, gwebsocket.CloseNormalClosure, gwebsocket.CloseAbnormalClosure, gwebsocket.CloseGoingAway)
}
