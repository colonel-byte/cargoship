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

package distrocfg

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	iofs "io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	hostos "github.com/colonel-byte/cargoship/src/types/os"
	"github.com/k0sproject/dig"
	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"gopkg.in/yaml.v3"
)

func testLoggerContext() (context.Context, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	l := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return logger.WithContext(context.Background(), l), buf
}

func TestValidateEngineConfigKnownVersionDropsUnknownKeys(t *testing.T) {
	ctx, buf := testLoggerContext()
	d := &RancherCommon{Common: Common{ID: "k3s"}}

	cfg := dig.Mapping{
		"cluster-cidr":  []string{"10.42.0.0/16"},
		"node-name":     "host-1",
		"totally-typod": "value",
	}

	d.validateEngineConfig(ctx, "1.35.3-k3s1", true, cfg)

	require.Contains(t, cfg, "cluster-cidr")
	require.Contains(t, cfg, "node-name")
	require.NotContains(t, cfg, "totally-typod")

	out := buf.String()
	require.NotContains(t, out, `key=cluster-cidr`)
	require.NotContains(t, out, `key=node-name`)
	require.Contains(t, out, "engine config key not recognized")
	require.Contains(t, out, `key=totally-typod`)
	require.NotContains(t, out, "level=WARN")
}

func TestValidateEngineConfigUnknownVersionBlindlyKeepsAllKeys(t *testing.T) {
	ctx, buf := testLoggerContext()
	d := &RancherCommon{Common: Common{ID: "k3s"}}

	cfg := dig.Mapping{"anything": "goes", "totally-typod": "value"}

	d.validateEngineConfig(ctx, "9.99.99-k3s1", true, cfg)

	require.Equal(t, dig.Mapping{"anything": "goes", "totally-typod": "value"}, cfg)

	out := buf.String()
	require.Contains(t, out, "no generated engine config schema for this distro/version")
	require.NotContains(t, out, "engine config key not recognized")
}

func TestValidateEngineConfigChecksAgentVsServerTarget(t *testing.T) {
	// "cluster-cidr" is a server-only k3s flag: valid for a controller, unrecognized (and
	// dropped) on an agent-only node.
	serverCfg := dig.Mapping{"cluster-cidr": []string{"10.42.0.0/16"}}
	serverCtx, serverBuf := testLoggerContext()
	d := &RancherCommon{Common: Common{ID: "k3s"}}
	d.validateEngineConfig(serverCtx, "1.35.3-k3s1", true, serverCfg)
	require.Contains(t, serverCfg, "cluster-cidr")
	require.NotContains(t, serverBuf.String(), "engine config key not recognized")

	agentCfg := dig.Mapping{"cluster-cidr": []string{"10.42.0.0/16"}}
	agentCtx, agentBuf := testLoggerContext()
	d.validateEngineConfig(agentCtx, "1.35.3-k3s1", false, agentCfg)
	require.NotContains(t, agentCfg, "cluster-cidr")
	require.Contains(t, agentBuf.String(), "engine config key not recognized")
	require.Contains(t, agentBuf.String(), `key=cluster-cidr`)
}

func TestValidateEngineConfigWarnsOnUnknownAddonButKeepsIt(t *testing.T) {
	ctx, buf := testLoggerContext()
	d := &RancherCommon{Common: Common{ID: "k3s"}}

	cfg := dig.Mapping{"disable": []any{"traefik", "not-a-component"}}

	d.validateEngineConfig(ctx, "1.35.3-k3s1", true, cfg)

	// Warned about, but written through: dropping it would deploy a component the user
	// explicitly asked to be without.
	require.Equal(t, dig.Mapping{"disable": []any{"traefik", "not-a-component"}}, cfg)

	out := buf.String()
	require.Contains(t, out, "level=WARN")
	require.Contains(t, out, "does not package")
	require.Contains(t, out, "component=not-a-component")
	require.NotContains(t, out, "component=traefik")
}

func TestValidateEngineConfigRKE2ChartsFromTheCNIUnionAreKnown(t *testing.T) {
	// rke2-ingress-nginx and rke2-canal are absent from rke2's own DisableItems -- they only
	// become valid through the chart union disableExceptSelected builds, which is exactly what
	// example/rke2-*/distro.yaml relies on.
	ctx, buf := testLoggerContext()
	d := &RancherCommon{Common: Common{ID: "rke2"}}

	cfg := dig.Mapping{"disable": []any{"rke2-ingress-nginx", "rke2-canal"}}

	d.validateEngineConfig(ctx, "1.35.8-rke2r1", true, cfg)

	require.NotContains(t, buf.String(), "does not package")
}

func TestValidateEngineConfigSkipsAddonCheckOnAgents(t *testing.T) {
	// "disable" is a server-only flag, so on an agent the key check has already removed it and
	// there is nothing left to warn about.
	ctx, buf := testLoggerContext()
	d := &RancherCommon{Common: Common{ID: "k3s"}}

	cfg := dig.Mapping{"disable": []any{"not-a-component"}}

	d.validateEngineConfig(ctx, "1.35.3-k3s1", false, cfg)

	require.NotContains(t, cfg, "disable")
	require.NotContains(t, buf.String(), "does not package")
}

func TestBuildRegistriesConfigNoAuth(t *testing.T) {
	registries := []cluster.ZarfClusterRegistries{
		{
			Name: "docker.io",
			Proxy: &cluster.ZarfClusterRegistryProxy{
				URL: "mirror-docker-hub.example.com",
			},
		},
	}

	got := buildRegistriesConfig(registries)

	want := dig.Mapping{
		keyMirrors: dig.Mapping{
			"docker.io": dig.Mapping{
				keyEndpoint: []string{"https://mirror-docker-hub.example.com"},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRegistriesConfig() = %+v, want %+v", got, want)
	}
}

func TestBuildRegistriesConfigWithAuth(t *testing.T) {
	registries := []cluster.ZarfClusterRegistries{
		{
			Name: "ghcr.io",
			Proxy: &cluster.ZarfClusterRegistryProxy{
				URL: "mirror-ghcr.example.com",
			},
			Authentication: cluster.ZarfClusterRegistryAuth{
				Token: "tok",
			},
		},
	}

	got := buildRegistriesConfig(registries)

	want := dig.Mapping{
		keyMirrors: dig.Mapping{
			"ghcr.io": dig.Mapping{
				keyEndpoint: []string{"https://mirror-ghcr.example.com"},
			},
		},
		keyConfigs: dig.Mapping{
			"mirror-ghcr.example.com": dig.Mapping{
				keyAuth: dig.Mapping{
					keyIdentityToken: "tok",
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRegistriesConfig() = %+v, want %+v", got, want)
	}
}

// A username and password are encoded into one base64 credential rather than written as two
// keys, so the node never carries the password in plain text.
func TestBuildRegistriesConfigWithUserPass(t *testing.T) {
	registries := []cluster.ZarfClusterRegistries{
		{
			Name: "quay.io",
			Proxy: &cluster.ZarfClusterRegistryProxy{
				URL: "mirror-quay.example.com",
			},
			Authentication: cluster.ZarfClusterRegistryAuth{
				Username: "robot",
				Password: "secretpassword",
			},
		},
	}

	got := buildRegistriesConfig(registries)

	want := dig.Mapping{
		keyMirrors: dig.Mapping{
			"quay.io": dig.Mapping{
				keyEndpoint: []string{"https://mirror-quay.example.com"},
			},
		},
		keyConfigs: dig.Mapping{
			"mirror-quay.example.com": dig.Mapping{
				keyAuth: dig.Mapping{
					keyAuth: base64.StdEncoding.EncodeToString([]byte("robot:secretpassword")),
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRegistriesConfig() = %+v, want %+v", got, want)
	}
}

// The auth keys are the ones wharfie unmarshals, and it ignores anything else without
// complaining, so a rename here is a silent loss of credentials on every node.
func TestRegistryAuthKeyNames(t *testing.T) {
	if keyUsername != "username" || keyPassword != "password" || keyAuth != "auth" || keyIdentityToken != "identity_token" {
		t.Errorf("auth keys = %q/%q/%q/%q, want username/password/auth/identity_token",
			keyUsername, keyPassword, keyAuth, keyIdentityToken)
	}
	if keyCAFile != "ca_file" || keyCertFile != "cert_file" || keyKeyFile != "key_file" || keyInsecureSkipVerify != "insecure_skip_verify" {
		t.Errorf("tls keys = %q/%q/%q/%q, want ca_file/cert_file/key_file/insecure_skip_verify",
			keyCAFile, keyCertFile, keyKeyFile, keyInsecureSkipVerify)
	}
}

func TestRegistryAuth(t *testing.T) {
	cases := map[string]struct {
		auth cluster.ZarfClusterRegistryAuth
		want dig.Mapping
	}{
		"user and password become one base64 credential under auth": {
			auth: cluster.ZarfClusterRegistryAuth{Username: "robot", Password: "secretpassword"},
			want: dig.Mapping{keyAuth: base64.StdEncoding.EncodeToString([]byte("robot:secretpassword"))},
		},
		"a token is passed through as given, under identity_token": {
			auth: cluster.ZarfClusterRegistryAuth{Token: "tok"},
			want: dig.Mapping{keyIdentityToken: "tok"},
		},
		"a token wins over a user and password": {
			auth: cluster.ZarfClusterRegistryAuth{Username: "robot", Password: "secretpassword", Token: "tok"},
			want: dig.Mapping{keyIdentityToken: "tok"},
		},
		"half a credential cannot be encoded, so it is written as it is": {
			auth: cluster.ZarfClusterRegistryAuth{Username: "robot"},
			want: dig.Mapping{keyUsername: "robot"},
		},
		"no credentials produce no auth entry": {
			auth: cluster.ZarfClusterRegistryAuth{},
			want: dig.Mapping{},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, registryAuth(tc.auth))
		})
	}
}

func TestBuildRegistriesConfigWithRewrite(t *testing.T) {
	registries := []cluster.ZarfClusterRegistries{
		{
			Name: "docker.io",
			Proxy: &cluster.ZarfClusterRegistryProxy{
				URL: "mirror-docker-hub.example.com",
				Rewrite: map[string]string{
					"^rancher/(.*)": "mirrorproject/rancher-images/$1",
				},
			},
		},
	}

	got := buildRegistriesConfig(registries)

	want := dig.Mapping{
		keyMirrors: dig.Mapping{
			"docker.io": dig.Mapping{
				keyEndpoint: []string{"https://mirror-docker-hub.example.com"},
				keyRewrite: map[string]string{
					"^rancher/(.*)": "mirrorproject/rancher-images/$1",
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRegistriesConfig() = %+v, want %+v", got, want)
	}
}

func TestBuildRegistriesConfigMultiple(t *testing.T) {
	registries := []cluster.ZarfClusterRegistries{
		{
			Name:  "docker.io",
			Proxy: &cluster.ZarfClusterRegistryProxy{URL: "mirror-docker-hub.example.com"},
		},
		{
			Name:  "quay.io",
			Proxy: &cluster.ZarfClusterRegistryProxy{URL: "mirror-quay.example.com"},
			Authentication: cluster.ZarfClusterRegistryAuth{
				Username: "user",
			},
		},
	}

	got := buildRegistriesConfig(registries)

	mirrors, ok := got[keyMirrors].(dig.Mapping)
	if !ok || len(mirrors) != 2 {
		t.Fatalf("buildRegistriesConfig() mirrors = %+v, want 2 entries", got[keyMirrors])
	}
	configs, ok := got[keyConfigs].(dig.Mapping)
	if !ok || len(configs) != 1 {
		t.Fatalf("buildRegistriesConfig() configs = %+v, want 1 entry", got[keyConfigs])
	}
	if _, ok := configs["mirror-quay.example.com"]; !ok {
		t.Fatalf("buildRegistriesConfig() configs = %+v, want entry for mirror-quay.example.com", configs)
	}
}

func TestBuildRegistriesConfigEmpty(t *testing.T) {
	got := buildRegistriesConfig(nil)

	want := dig.Mapping{}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRegistriesConfig() = %+v, want %+v", got, want)
	}
}

// fakeHost stands in for a host the distro modules act on: its filesystem, its init system,
// and the one Configurer method they still reach for. One value backs all three, so a test
// sets up the files and services it needs in a single place and reads back what was written.
type fakeHost struct {
	files     map[string]string
	fileExist map[string]bool
	writeErr  map[string]error

	serviceRunning bool
	stopServiceErr error

	deleteFileErr error

	commandExist bool

	touched  []string
	touchErr error

	lookPathResult string
	lookPathErr    error

	longHostname string
}

// attach wires h to f and returns it, so a test can set the host fields it cares about in the
// literal and still get a host whose file and service operations land in f.
func (f *fakeHost) attach(h *cluster.ZarfHost) *cluster.ZarfHost {
	h.Configurer = &fakeConfigurer{fake: f}
	h.SetFS(&fakeFS{fake: f})
	h.SetServices(&fakeServices{fake: f})
	return h
}

// fakeFS is the filesystem side of a fakeHost. Only the operations the distro modules make are
// implemented; the embedded interface is nil, so anything else panics and says so.
type fakeFS struct {
	remotefs.FS

	fake *fakeHost
}

func (f *fakeFS) FileExist(path string) bool {
	return f.fake.fileExist[path]
}

func (f *fakeFS) WriteFile(path string, data []byte, _ iofs.FileMode) error {
	if err, ok := f.fake.writeErr[path]; ok {
		return err
	}
	if f.fake.files == nil {
		f.fake.files = map[string]string{}
	}
	f.fake.files[path] = string(data)
	return nil
}

func (f *fakeFS) ReadFile(path string) ([]byte, error) {
	return []byte(f.fake.files[path]), nil
}

func (f *fakeFS) Remove(path string) error {
	if f.fake.deleteFileErr != nil {
		return f.fake.deleteFileErr
	}
	delete(f.fake.files, path)
	return nil
}

func (f *fakeFS) CommandExist(_ string) bool {
	return f.fake.commandExist
}

func (f *fakeFS) Touch(path string, _ ...time.Time) error {
	f.fake.touched = append(f.fake.touched, path)
	return f.fake.touchErr
}

func (f *fakeFS) LookPath(_ string) (string, error) {
	return f.fake.lookPathResult, f.fake.lookPathErr
}

// fakeServices is the init system side of a fakeHost. The distro modules only ask whether a
// service is running and stop it, so the rest report as unimplemented rather than silently
// succeeding.
type fakeServices struct {
	fake *fakeHost
}

func (f *fakeServices) ServiceIsRunning(_ context.Context, _ string) bool {
	return f.fake.serviceRunning
}

func (f *fakeServices) StopService(_ context.Context, _ string) error {
	return f.fake.stopServiceErr
}

func (f *fakeServices) StartService(_ context.Context, name string) error {
	return fmt.Errorf("fakeServices: unexpected start of %s", name)
}

func (f *fakeServices) RestartService(_ context.Context, name string) error {
	return fmt.Errorf("fakeServices: unexpected restart of %s", name)
}

func (f *fakeServices) EnableService(_ context.Context, name string) error {
	return fmt.Errorf("fakeServices: unexpected enable of %s", name)
}

// fakeConfigurer implements hostos.Configurer, overriding only the method the distro modules
// still call on it. The nil-embedded interface satisfies the type while making any other call
// panic with the method name.
type fakeConfigurer struct {
	hostos.Configurer

	fake *fakeHost
}

func (c *fakeConfigurer) LongHostname(_ hostos.Host) string {
	return c.fake.longHostname
}

func newTestRancher() *RancherCommon {
	return &RancherCommon{
		Common: Common{
			Binary:    "k3s",
			BinaryDir: "/usr/local/bin",
			Config:    "/etc/rancher/k3s/config.yaml",
			Data:      "/var/lib/rancher/k3s",
			Token:     "/var/lib/rancher/k3s/server/token",
		},
	}
}

func parseWrittenYAML(t *testing.T, files map[string]string, path string) dig.Mapping {
	t.Helper()
	content, ok := files[path]
	if !ok {
		t.Fatalf("expected file %s to be written, files = %+v", path, files)
	}
	var got dig.Mapping
	if err := yaml.Unmarshal([]byte(content), &got); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", path, err)
	}
	return got
}

func TestConfigureEngineControllerLeaderNoTokenFiles(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	run := cluster.ZarfRuntimeMeta{
		ControllerTLS:   []string{"10.0.0.1"},
		ControllerToken: "ctoken",
		AgentToken:      "atoken",
		LoadBalancer:    "lb.example.com",
	}
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.ZarfHost{Role: cluster.RoleController, Hostname: "node1"})
	host.Metadata.IsLeader = true

	if err := d.ConfigureEngine(context.Background(), host, run, dis); err != nil {
		t.Fatalf("ConfigureEngine() error = %v", err)
	}

	if got := cfg.files[d.JoinTokenPath()]; got != "ctoken" {
		t.Errorf("controller token file = %q, want %q", got, "ctoken")
	}
	if got := cfg.files[d.JoinTokenPathAgent()]; got != "atoken" {
		t.Errorf("agent token file = %q, want %q", got, "atoken")
	}

	got := parseWrittenYAML(t, cfg.files, d.Config)
	if got.DigString(keyNodeName) != "node1" {
		t.Errorf("config.yaml node-name = %q, want %q", got.DigString(keyNodeName), "node1")
	}
	if got.DigString(keyDataDir) != d.Data {
		t.Errorf("config.yaml data-dir = %q, want %q", got.DigString(keyDataDir), d.Data)
	}
	if got.DigString(keyTokenFile) != d.JoinTokenPath() {
		t.Errorf("config.yaml token-file = %q, want %q", got.DigString(keyTokenFile), d.JoinTokenPath())
	}
	if got.DigString(keyAgentToken) != d.JoinTokenPathAgent() {
		t.Errorf("config.yaml agent-token-file = %q, want %q", got.DigString(keyAgentToken), d.JoinTokenPathAgent())
	}
	tls, ok := got[keyTLS].([]any)
	if !ok || len(tls) != 1 || tls[0] != "10.0.0.1" {
		t.Errorf("config.yaml tls-san = %+v, want [10.0.0.1]", got[keyTLS])
	}
}

func TestConfigureEngineControllerFollowerExistingTokenFile(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	run := cluster.ZarfRuntimeMeta{LoadBalancer: "lb.example.com"}

	leaderCfg := &fakeHost{longHostname: "leader.example.com"}
	leaderHost := leaderCfg.attach(&cluster.ZarfHost{})
	run.Leader = leaderHost

	cfg := &fakeHost{
		fileExist: map[string]bool{d.JoinTokenPath(): true},
		files:     map[string]string{d.JoinTokenPath(): "existing-token"},
	}
	host := cfg.attach(&cluster.ZarfHost{Role: cluster.RoleController, Hostname: "node2"})
	host.Metadata.IsLeader = false

	if err := d.ConfigureEngine(context.Background(), host, run, dis); err != nil {
		t.Fatalf("ConfigureEngine() error = %v", err)
	}

	if got := cfg.files[d.JoinTokenPath()]; got != "existing-token" {
		t.Errorf("controller token file overwritten, got %q, want %q", got, "existing-token")
	}

	got := parseWrittenYAML(t, cfg.files, d.Config)
	if want := "https://leader.example.com:9345"; got.DigString(keyServer) != want {
		t.Errorf("config.yaml server = %q, want %q", got.DigString(keyServer), want)
	}
}

func TestConfigureEngineWorkerStripsControllerArgs(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	dis.Spec.Config.Engine = dig.Mapping{
		config.EngineConfig: dig.Mapping{
			keyKubeAPI:       "foo",
			keyKubeConMan:    "bar",
			keyKubeScheduler: "baz",
			keyETCD:          "qux",
		},
	}
	run := cluster.ZarfRuntimeMeta{LoadBalancer: "lb.example.com", AgentToken: "atoken"}
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.ZarfHost{Role: cluster.RoleWorker, Hostname: "node3"})

	if err := d.ConfigureEngine(context.Background(), host, run, dis); err != nil {
		t.Fatalf("ConfigureEngine() error = %v", err)
	}

	if _, written := cfg.files[d.JoinTokenPath()]; written {
		t.Errorf("worker should not write controller token file %s", d.JoinTokenPath())
	}

	got := parseWrittenYAML(t, cfg.files, d.Config)
	if want := "https://lb.example.com:9345"; got.DigString(keyServer) != want {
		t.Errorf("config.yaml server = %q, want %q", got.DigString(keyServer), want)
	}
	if got.DigString(keyTokenFile) != d.JoinTokenPathAgent() {
		t.Errorf("config.yaml token-file = %q, want %q", got.DigString(keyTokenFile), d.JoinTokenPathAgent())
	}
	for _, k := range []string{keyKubeAPI, keyKubeConMan, keyKubeScheduler, keyETCD} {
		if _, ok := got[k]; ok {
			t.Errorf("config.yaml still contains controller-only key %q", k)
		}
	}
}

func TestConfigureEngineWritesManifests(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	dis.Spec.Config.Engine = dig.Mapping{
		config.EngineManifest: dig.Mapping{
			"my-chart": "foo: bar",
		},
	}
	run := cluster.ZarfRuntimeMeta{}
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.ZarfHost{Role: cluster.RoleController, Hostname: "node1"})
	host.Metadata.IsLeader = true

	if err := d.ConfigureEngine(context.Background(), host, run, dis); err != nil {
		t.Fatalf("ConfigureEngine() error = %v", err)
	}

	path := filepath.Join(d.Data, "server/manifests/my-chart-config.yaml")
	got := parseWrittenYAML(t, cfg.files, path)
	if got.DigString(keyKind) != "HelmChartConfig" {
		t.Errorf("manifest kind = %q, want %q", got.DigString(keyKind), "HelmChartConfig")
	}
	if got.DigString(keyAPIVersion) != "helm.cattle.io/v1" {
		t.Errorf("manifest apiVersion = %q, want %q", got.DigString(keyAPIVersion), "helm.cattle.io/v1")
	}
	if got.DigString(keyMetadata, "name") != "my-chart" {
		t.Errorf("manifest metadata.name = %q, want %q", got.DigString(keyMetadata, "name"), "my-chart")
	}
	if got.DigString(keySpec, "valuesContent") != "foo: bar" {
		t.Errorf("manifest spec.valuesContent = %q, want %q", got.DigString(keySpec, "valuesContent"), "foo: bar")
	}
}

func TestConfigureEngineWritesAudit(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	dis.Spec.Config.Engine = dig.Mapping{
		config.EngineAudit: dig.Mapping{
			"rules": []string{"foo"},
		},
	}
	run := cluster.ZarfRuntimeMeta{}
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.ZarfHost{Role: cluster.RoleWorker, Hostname: "node1"})

	if err := d.ConfigureEngine(context.Background(), host, run, dis); err != nil {
		t.Fatalf("ConfigureEngine() error = %v", err)
	}

	auditPath := filepath.Join(filepath.Dir(d.Config), "audit.yaml")
	got := parseWrittenYAML(t, cfg.files, auditPath)
	if got.DigString(keyKind) != "Policy" {
		t.Errorf("audit.yaml kind = %q, want %q", got.DigString(keyKind), "Policy")
	}
	if got.DigString(keyAPIVersion) != "audit.k8s.io/v1" {
		t.Errorf("audit.yaml apiVersion = %q, want %q", got.DigString(keyAPIVersion), "audit.k8s.io/v1")
	}

	configYAML := parseWrittenYAML(t, cfg.files, d.Config)
	if configYAML.DigString(keyAudit) != auditPath {
		t.Errorf("config.yaml audit-policy-file = %q, want %q", configYAML.DigString(keyAudit), auditPath)
	}
}

func TestConfigureEngineWritesPSS(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	dis.Spec.Config.Engine = dig.Mapping{
		config.EnginePSS: dig.Mapping{
			"defaults": dig.Mapping{"enforce": "restricted"},
		},
	}
	run := cluster.ZarfRuntimeMeta{}
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.ZarfHost{Role: cluster.RoleWorker, Hostname: "node1"})

	if err := d.ConfigureEngine(context.Background(), host, run, dis); err != nil {
		t.Fatalf("ConfigureEngine() error = %v", err)
	}

	pssPath := filepath.Join(filepath.Dir(d.Config), "pss.yaml")
	got := parseWrittenYAML(t, cfg.files, pssPath)
	if got.DigString(keyKind) != "AdmissionConfiguration" {
		t.Errorf("pss.yaml kind = %q, want %q", got.DigString(keyKind), "AdmissionConfiguration")
	}
	if got.DigString(keyAPIVersion) != "apiserver.config.k8s.io/v1" {
		t.Errorf("pss.yaml apiVersion = %q, want %q", got.DigString(keyAPIVersion), "apiserver.config.k8s.io/v1")
	}

	configYAML := parseWrittenYAML(t, cfg.files, d.Config)
	if configYAML.DigString(keyPodSec) != pssPath {
		t.Errorf("config.yaml pod-security-admission-config-file = %q, want %q", configYAML.DigString(keyPodSec), pssPath)
	}
}

func TestConfigureEngineWritesRegistries(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	run := cluster.ZarfRuntimeMeta{
		Registries: []cluster.ZarfClusterRegistries{
			{
				Name:  "docker.io",
				Proxy: &cluster.ZarfClusterRegistryProxy{URL: "mirror.example.com"},
			},
		},
	}
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.ZarfHost{Role: cluster.RoleWorker, Hostname: "node1"})

	if err := d.ConfigureEngine(context.Background(), host, run, dis); err != nil {
		t.Fatalf("ConfigureEngine() error = %v", err)
	}

	registriesPath := filepath.Join(filepath.Dir(d.Config), "registries.yaml")
	got := parseWrittenYAML(t, cfg.files, registriesPath)
	mirrors, ok := got[keyMirrors].(dig.Mapping)
	if !ok {
		t.Fatalf("registries.yaml mirrors = %+v, want a mapping", got[keyMirrors])
	}
	docker, ok := mirrors["docker.io"].(dig.Mapping)
	if !ok {
		t.Fatalf("registries.yaml mirrors[docker.io] = %+v, want a mapping", mirrors["docker.io"])
	}
	endpoints, ok := docker[keyEndpoint].([]any)
	if !ok || len(endpoints) != 1 || endpoints[0] != "https://mirror.example.com" {
		t.Errorf("registries.yaml mirrors[docker.io].endpoint = %+v, want [https://mirror.example.com]", docker[keyEndpoint])
	}
}

func TestConfigureEngineNoRegistriesSkipsFile(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	run := cluster.ZarfRuntimeMeta{}
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.ZarfHost{Role: cluster.RoleWorker, Hostname: "node1"})

	if err := d.ConfigureEngine(context.Background(), host, run, dis); err != nil {
		t.Fatalf("ConfigureEngine() error = %v", err)
	}

	registriesPath := filepath.Join(filepath.Dir(d.Config), "registries.yaml")
	if _, written := cfg.files[registriesPath]; written {
		t.Errorf("registries.yaml should not be written when no registries are configured")
	}
}

// TestConfigureEngineWritesRegistriesGolden drives the full registries.yaml builder
// through ConfigureEngine with a mirror+rewrite registry and an authenticated registry,
// then diffs the written file byte-for-byte against testdata/registries.yaml -- update
// that fixture (and reread the diff) if a deliberate change to buildRegistriesConfig's
// output shape is made.
func TestConfigureEngineWritesRegistriesGolden(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	run := cluster.ZarfRuntimeMeta{
		Registries: []cluster.ZarfClusterRegistries{
			{
				Name: "docker.io",
				Proxy: &cluster.ZarfClusterRegistryProxy{
					URL: "mirror-docker-hub.example.com",
					Rewrite: map[string]string{
						"^rancher/(.*)": "mirrorproject/rancher-images/$1",
					},
				},
			},
			{
				Name:  "ghcr.io",
				Proxy: &cluster.ZarfClusterRegistryProxy{URL: "mirror-ghcr.example.com"},
				Authentication: cluster.ZarfClusterRegistryAuth{
					Token: "tok",
				},
			},
			{
				Name:  "quay.io",
				Proxy: &cluster.ZarfClusterRegistryProxy{URL: "mirror-quay.example.com"},
				Authentication: cluster.ZarfClusterRegistryAuth{
					Username: "robot",
					Password: "secretpassword",
				},
			},
		},
	}
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.ZarfHost{Role: cluster.RoleWorker, Hostname: "node1"})

	if err := d.ConfigureEngine(context.Background(), host, run, dis); err != nil {
		t.Fatalf("ConfigureEngine() error = %v", err)
	}

	registriesPath := filepath.Join(filepath.Dir(d.Config), "registries.yaml")
	got, written := cfg.files[registriesPath]
	if !written {
		t.Fatalf("expected file %s to be written, files = %+v", registriesPath, cfg.files)
	}

	want, err := os.ReadFile("testdata/registries.yaml")
	if err != nil {
		t.Fatalf("failed to read golden file: %v", err)
	}

	if got != string(want) {
		t.Errorf("registries.yaml = \n%s\nwant\n%s", got, want)
	}
}

func TestDesiredFilesEmpty(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	run := cluster.ZarfRuntimeMeta{}

	got, err := d.DesiredFiles(&cluster.ZarfHost{}, run, dis)
	if err != nil {
		t.Fatalf("DesiredFiles() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("DesiredFiles() = %+v, want empty map", got)
	}
}

func TestDesiredFilesIncludesDistroReleaseMetadata(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{
		Metadata: distro.ZarfDistroMetadata{
			Name:              "rancher-rke2-v1.31.0",
			Version:           "v1.31.0",
			AggregateChecksum: "abc123hash",
		},
		Spec: distro.ZarfDistroSpec{
			Type:    "rke2",
			Version: "v1.31.0+rke2r1",
			Config: distro.ZarfDistroConfig{
				ImagesConfig: distro.ZarfDistroImageConfig{
					Images: []string{"rancher/rke2-runtime:v1.31.0", "rancher/pause:3.9"},
				},
				Engine: dig.Mapping{
					config.EngineAudit: dig.Mapping{"rules": []string{"audit"}},
				},
			},
		},
	}
	run := cluster.ZarfRuntimeMeta{}

	got, err := d.DesiredFiles(&cluster.ZarfHost{}, run, dis)
	require.NoError(t, err)

	metaFile, ok := got[DistroReleaseFile]
	require.True(t, ok, "DistroReleaseFile must be present in DesiredFiles")
	require.Equal(t, modeConfigFile, metaFile.Mode)

	var parsed struct {
		Name              string   `json:"name"`
		DistroVersion     string   `json:"distroVersion"`
		AggregateChecksum string   `json:"aggregateChecksum"`
		Images            []string `json:"images"`
		ManagedFiles      []string `json:"managedFiles"`
	}
	err = json.Unmarshal(metaFile.Content, &parsed)
	require.NoError(t, err)
	require.Equal(t, "rancher-rke2-v1.31.0", parsed.Name)
	require.Equal(t, "v1.31.0+rke2r1", parsed.DistroVersion)
	require.Equal(t, "abc123hash", parsed.AggregateChecksum)
	require.Equal(t, []string{"rancher/rke2-runtime:v1.31.0", "rancher/pause:3.9"}, parsed.Images)
	require.Contains(t, parsed.ManagedFiles, filepath.Join(filepath.Dir(d.Config), "audit.yaml"))
}

func TestDesiredFilesRegistriesAuditPSS(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	dis.Spec.Config.Engine = dig.Mapping{
		config.EngineAudit: dig.Mapping{
			"rules": []string{"foo"},
		},
		config.EnginePSS: dig.Mapping{
			"defaults": dig.Mapping{"enforce": "restricted"},
		},
	}
	run := cluster.ZarfRuntimeMeta{
		Registries: []cluster.ZarfClusterRegistries{
			{
				Name:  "docker.io",
				Proxy: &cluster.ZarfClusterRegistryProxy{URL: "mirror.example.com"},
			},
		},
	}

	got, err := d.DesiredFiles(&cluster.ZarfHost{}, run, dis)
	if err != nil {
		t.Fatalf("DesiredFiles() error = %v", err)
	}

	registriesPath := filepath.Join(filepath.Dir(d.Config), "registries.yaml")
	auditPath := filepath.Join(filepath.Dir(d.Config), "audit.yaml")
	pssPath := filepath.Join(filepath.Dir(d.Config), "pss.yaml")

	if len(got) != 3 {
		t.Fatalf("DesiredFiles() = %d entries, want 3: %+v", len(got), got)
	}

	var auditYAML, pssYAML dig.Mapping
	if err := yaml.Unmarshal(got[auditPath].Content, &auditYAML); err != nil {
		t.Fatalf("failed to unmarshal audit.yaml: %v", err)
	}
	if auditYAML.DigString(keyKind) != "Policy" || auditYAML.DigString(keyAPIVersion) != "audit.k8s.io/v1" {
		t.Errorf("audit.yaml = %+v, want kind=Policy apiVersion=audit.k8s.io/v1", auditYAML)
	}

	if err := yaml.Unmarshal(got[pssPath].Content, &pssYAML); err != nil {
		t.Fatalf("failed to unmarshal pss.yaml: %v", err)
	}
	if pssYAML.DigString(keyKind) != "AdmissionConfiguration" || pssYAML.DigString(keyAPIVersion) != "apiserver.config.k8s.io/v1" {
		t.Errorf("pss.yaml = %+v, want kind=AdmissionConfiguration apiVersion=apiserver.config.k8s.io/v1", pssYAML)
	}

	wantRegistries, err := marshalRegistriesYAML(buildRegistriesConfig(run.Registries))
	if err != nil {
		t.Fatalf("marshalRegistriesYAML() error = %v", err)
	}
	if string(got[registriesPath].Content) != string(wantRegistries) {
		t.Errorf("registries.yaml = %s, want %s", got[registriesPath].Content, wantRegistries)
	}
}

func TestGetClusterCIDRDefaults(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}

	got := d.GetClusterCIDR(dis)

	want := []string{"10.42.0.0/16", "10.43.0.0/16"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetClusterCIDR() = %+v, want %+v", got, want)
	}
}

func TestGetClusterCIDROverridden(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	dis.Spec.Config.Engine = dig.Mapping{
		config.EngineConfig: dig.Mapping{
			keyCIDRPod: "192.168.0.0/16",
			keyCIDRSVC: "192.169.0.0/16",
		},
	}

	got := d.GetClusterCIDR(dis)

	want := []string{"192.168.0.0/16", "192.169.0.0/16"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetClusterCIDR() = %+v, want %+v", got, want)
	}
}

func TestJoinTokenPathAgent(t *testing.T) {
	d := newTestRancher()

	got := d.JoinTokenPathAgent()

	want := filepath.Join(filepath.Dir(d.Token), "agent-token")
	if got != want {
		t.Fatalf("JoinTokenPathAgent() = %q, want %q", got, want)
	}
}

func TestDistroCmdf(t *testing.T) {
	d := newTestRancher()

	got := d.DistroCmdf("etcd-snapshot save --name %s", "backup")

	want := "/usr/local/bin/k3s etcd-snapshot save --name backup"
	if got != want {
		t.Fatalf("DistroCmdf() = %q, want %q", got, want)
	}
}

func TestRunningVersionLookPathError(t *testing.T) {
	d := newTestRancher()
	cfg := &fakeHost{lookPathErr: errors.New("not found")}
	host := cfg.attach(&cluster.ZarfHost{})

	_, err := d.RunningVersion(host)

	if !errors.Is(err, ErrVersionNotDetected) {
		t.Fatalf("RunningVersion() error = %v, want %v", err, ErrVersionNotDetected)
	}
}

func TestRunningVersionExecNotConnected(t *testing.T) {
	d := newTestRancher()
	cfg := &fakeHost{lookPathResult: "/usr/local/bin/k3s"}
	host := cfg.attach(&cluster.ZarfHost{})

	_, err := d.RunningVersion(host)

	if !errors.Is(err, ErrVersionNotDetected) {
		t.Fatalf("RunningVersion() error = %v, want %v", err, ErrVersionNotDetected)
	}
}

func TestStopServiceNotRunningNoCacheNoKillall(t *testing.T) {
	d := newTestRancher()
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.ZarfHost{})

	if err := d.stopService(host, "k3s", "k3s-killall.sh"); err != nil {
		t.Fatalf("stopService() error = %v", err)
	}
	if len(cfg.touched) != 0 {
		t.Errorf("stopService() touched files = %+v, want none", cfg.touched)
	}
}

func TestStopServiceStopServiceError(t *testing.T) {
	d := newTestRancher()
	wantErr := errors.New("stop failed")
	cfg := &fakeHost{fileExist: map[string]bool{}, serviceRunning: true, stopServiceErr: wantErr}
	host := cfg.attach(&cluster.ZarfHost{})

	err := d.stopService(host, "k3s", "k3s-killall.sh")

	if !errors.Is(err, wantErr) {
		t.Fatalf("stopService() error = %v, want %v", err, wantErr)
	}
}

func TestStopServiceDeleteFileError(t *testing.T) {
	d := newTestRancher()
	wantErr := errors.New("delete failed")
	cacheFile := d.Data + "/agent/images/.cache.json"
	cfg := &fakeHost{
		fileExist:     map[string]bool{cacheFile: true},
		deleteFileErr: wantErr,
	}
	host := cfg.attach(&cluster.ZarfHost{})

	err := d.stopService(host, "k3s", "k3s-killall.sh")

	if !errors.Is(err, wantErr) {
		t.Fatalf("stopService() error = %v, want %v", err, wantErr)
	}
}

func TestStopServiceCacheDeletedNoKillallTouches(t *testing.T) {
	d := newTestRancher()
	cacheFile := d.Data + "/agent/images/.cache.json"
	cfg := &fakeHost{
		fileExist: map[string]bool{cacheFile: true},
		files:     map[string]string{cacheFile: "{}"},
	}
	host := cfg.attach(&cluster.ZarfHost{})

	if err := d.stopService(host, "k3s", "k3s-killall.sh"); err != nil {
		t.Fatalf("stopService() error = %v", err)
	}
	if len(cfg.touched) != 1 || cfg.touched[0] != cacheFile {
		t.Errorf("stopService() touched = %+v, want [%s]", cfg.touched, cacheFile)
	}
}

func TestStopServiceKillallExistsPropagatesExecError(t *testing.T) {
	d := newTestRancher()
	cfg := &fakeHost{fileExist: map[string]bool{}, commandExist: true}
	host := cfg.attach(&cluster.ZarfHost{})

	err := d.stopService(host, "k3s", "k3s-killall.sh")

	if err == nil {
		t.Fatalf("stopService() error = nil, want an error from the unconnected host's Exec call")
	}
}

func TestRancherVersionRegex(t *testing.T) {
	tests := map[string]struct {
		output string
		want   string
	}{
		"k3s":  {output: "k3s version v1.36.4+k3s1 (0a1b2c3d)\ngo version go1.25.1\n", want: "v1.36.4+k3s1"},
		"rke2": {output: "rke2 version v1.35.8+rke2r1 (0a1b2c3d)\ngo version go1.25.1\n", want: "v1.35.8+rke2r1"},
		// The distro suffix is mandatory, which is why this regex belongs to the Rancher
		// distros rather than to every distro that embeds Common.
		"upstream kubelet": {output: "Kubernetes v1.35.3\n", want: ""},
		"no version":       {output: "command not found\n", want: ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.want, rancherVersionRegex.FindString(tt.output))
		})
	}
}

func TestCleanupPaths(t *testing.T) {
	tests := map[string]struct {
		distro Distro
		want   []string
	}{
		"k3s": {
			distro: &K3S{RancherCommon{Common{Config: "/etc/rancher/k3s/config.yaml", Data: "/var/lib/rancher/k3s"}}},
			want:   []string{"/var/lib/rancher/k3s", "/etc/rancher/k3s"},
		},
		"rke2": {
			distro: &RKE2{RancherCommon{Common{Config: "/etc/rancher/rke2/config.yaml", Data: "/var/lib/rancher/rke2"}}},
			want:   []string{"/var/lib/rancher/rke2", "/etc/rancher/rke2"},
		},
		"unset paths are not removable": {
			distro: &K3S{RancherCommon{Common{}}},
			want:   []string{},
		},
		"root is not removable": {
			distro: &K3S{RancherCommon{Common{Config: "/config.yaml", Data: "/"}}},
			want:   []string{},
		},
		"the same directory is only removed once": {
			distro: &K3S{RancherCommon{Common{Config: "/etc/rancher/k3s/config.yaml", Data: "/etc/rancher/k3s"}}},
			want:   []string{"/etc/rancher/k3s"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.distro.CleanupPaths())
		})
	}
}

// The mirror and configs keys are registry names and hosts, which can look like numbers,
// booleans, or the "*" wildcard, so they are written quoted.
func TestMarshalRegistriesYAMLQuotesKeys(t *testing.T) {
	registries := []cluster.ZarfClusterRegistries{
		{
			Name: "*",
			Proxy: &cluster.ZarfClusterRegistryProxy{
				URL:     "mirror.example.com:5000",
				Rewrite: map[string]string{"^rancher/(.*)": "mirrorproject/rancher-images/$1"},
			},
			Authentication: cluster.ZarfClusterRegistryAuth{Token: "tok"},
		},
	}

	got, err := marshalRegistriesYAML(buildRegistriesConfig(registries))
	if err != nil {
		t.Fatalf("marshalRegistriesYAML() error = %v", err)
	}

	for _, want := range []string{`"*":`, `"mirror.example.com:5000":`, `"^rancher/(.*)":`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("registries.yaml = %s, want it to contain %s", got, want)
		}
	}

	// Quoting is a rendering choice, so the file still has to parse back to the same keys.
	var round map[string]map[string]any
	if err := yaml.Unmarshal(got, &round); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if _, ok := round["mirrors"]["*"]; !ok {
		t.Errorf("mirrors = %+v, want a * entry", round["mirrors"])
	}
	if _, ok := round["configs"]["mirror.example.com:5000"]; !ok {
		t.Errorf("configs = %+v, want a mirror.example.com:5000 entry", round["configs"])
	}
}

func TestBuildRegistriesConfigKeysConfigsByHost(t *testing.T) {
	registries := []cluster.ZarfClusterRegistries{
		{
			Name:           "docker.io",
			Proxy:          &cluster.ZarfClusterRegistryProxy{URL: "https://mirror.example.com:5000/v2"},
			Authentication: cluster.ZarfClusterRegistryAuth{Username: "robot", Password: "secretpassword"},
		},
	}

	got := buildRegistriesConfig(registries)

	configs, ok := got[keyConfigs].(dig.Mapping)
	if !ok {
		t.Fatalf("buildRegistriesConfig() has no configs entry: %+v", got)
	}
	if _, ok := configs["mirror.example.com:5000"]; !ok {
		t.Errorf("configs keys = %+v, want an entry for mirror.example.com:5000", configs)
	}

	mirrors, ok := got[keyMirrors].(dig.Mapping)
	if !ok {
		t.Fatalf("buildRegistriesConfig() has no mirrors entry: %+v", got)
	}
	docker, ok := mirrors["docker.io"].(dig.Mapping)
	if !ok {
		t.Fatalf("mirrors has no docker.io entry: %+v", mirrors)
	}
	endpoint := docker[keyEndpoint]
	if !reflect.DeepEqual(endpoint, []string{"https://mirror.example.com:5000/v2"}) {
		t.Errorf("endpoint = %+v, want the URL as written", endpoint)
	}
}

// A registry can carry credentials without a mirror -- they authenticate a direct pull. The
// entry is keyed by the registry itself then, since that is where the pull goes.
func TestBuildRegistriesConfigWithoutProxy(t *testing.T) {
	registries := []cluster.ZarfClusterRegistries{
		{
			Name:           "registry.example.com",
			Authentication: cluster.ZarfClusterRegistryAuth{Token: "tok"},
		},
	}

	want := dig.Mapping{
		keyConfigs: dig.Mapping{
			"registry.example.com": dig.Mapping{
				keyAuth: dig.Mapping{keyIdentityToken: "tok"},
			},
		},
	}
	require.Equal(t, want, buildRegistriesConfig(registries),
		"a registry without a proxy gets credentials but no mirror")
}

func TestBuildRegistriesConfigWithTLS(t *testing.T) {
	insecure := true
	registries := []cluster.ZarfClusterRegistries{
		{
			Name: "private.registry.io",
			Proxy: &cluster.ZarfClusterRegistryProxy{
				URL: "mirror-private.example.com",
			},
			TLS: &cluster.ZarfClusterRegistryTLS{
				CAFile:             "/etc/ssl/certs/ca.pem",
				CertFile:           "/etc/ssl/certs/client.pem",
				KeyFile:            "/etc/ssl/certs/client-key.pem",
				InsecureSkipVerify: insecure,
			},
		},
	}

	got := buildRegistriesConfig(registries)

	want := dig.Mapping{
		keyMirrors: dig.Mapping{
			"private.registry.io": dig.Mapping{
				keyEndpoint: []string{"https://mirror-private.example.com"},
			},
		},
		keyConfigs: dig.Mapping{
			"mirror-private.example.com": dig.Mapping{
				keyTLSConfig: dig.Mapping{
					keyCAFile:             "/etc/ssl/certs/ca.pem",
					keyCertFile:           "/etc/ssl/certs/client.pem",
					keyKeyFile:            "/etc/ssl/certs/client-key.pem",
					keyInsecureSkipVerify: true,
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRegistriesConfig() = %+v, want %+v", got, want)
	}
}

const testCAPEM = `-----BEGIN CERTIFICATE-----
dGhpcyBpcyBub3QgYSByZWFsIGNlcnRpZmljYXRl
-----END CERTIFICATE-----
`

// An inline CA is written to its own file, and ca_file points at that file rather than at the
// certificate itself.
func TestBuildRegistriesConfigInlineCA(t *testing.T) {
	registries := []cluster.ZarfClusterRegistries{
		{
			Name:  "docker.io",
			Proxy: &cluster.ZarfClusterRegistryProxy{URL: "mirror.example.com:5000"},
			TLS:   &cluster.ZarfClusterRegistryTLS{CA: testCAPEM},
		},
	}

	got := buildRegistriesConfig(registries)

	configs, ok := got[keyConfigs].(dig.Mapping)
	require.True(t, ok, "buildRegistriesConfig() has no configs entry: %+v", got)
	entry, ok := configs["mirror.example.com:5000"].(dig.Mapping)
	require.True(t, ok, "configs has no entry for the mirror host: %+v", configs)
	require.Equal(t,
		dig.Mapping{keyCAFile: "/etc/cargoship/tls/mirror.example.com_5000.crt"},
		entry[keyTLSConfig],
		"ca_file points at the file cargoship writes the inline CA to")

	require.Equal(t,
		map[string][]byte{"/etc/cargoship/tls/mirror.example.com_5000.crt": []byte(testCAPEM)},
		registryCAFiles(registries))
}

// A CA path already on the host is used as given, and nothing extra is written.
func TestBuildRegistriesConfigCAFile(t *testing.T) {
	registries := []cluster.ZarfClusterRegistries{
		{
			Name: "nexus.example.com",
			TLS:  &cluster.ZarfClusterRegistryTLS{CAFile: "/etc/pki/ca.crt"},
		},
	}

	got := buildRegistriesConfig(registries)

	configs, ok := got[keyConfigs].(dig.Mapping)
	require.True(t, ok, "buildRegistriesConfig() has no configs entry: %+v", got)
	entry, ok := configs["nexus.example.com"].(dig.Mapping)
	require.True(t, ok, "configs has no entry for the registry: %+v", configs)
	require.Equal(t, dig.Mapping{keyCAFile: "/etc/pki/ca.crt"}, entry[keyTLSConfig])
	require.Empty(t, registryCAFiles(registries))
}

func TestRegistryCAPath(t *testing.T) {
	cases := map[string]string{
		"mirror.example.com":      "/etc/cargoship/tls/mirror.example.com.crt",
		"mirror.example.com:5000": "/etc/cargoship/tls/mirror.example.com_5000.crt",
		"*":                       "/etc/cargoship/tls/_.crt",
		"../../etc/shadow":        "/etc/cargoship/tls/.._.._etc_shadow.crt",
	}
	for host, want := range cases {
		if got := registryCAPath(host); got != want {
			t.Errorf("registryCAPath(%q) = %q, want %q", host, got, want)
		}
	}
}

// TestConfigureEngineWritesRegistriesTLSGolden covers the tls half of the file the auth golden
// does not: an inline CA that becomes a ca_file path, CA and client certificate paths already on
// the host, and verification turned off. It diffs byte-for-byte against
// testdata/registries-tls.yaml -- update that fixture if a deliberate change to the output shape
// is made.
func TestConfigureEngineWritesRegistriesTLSGolden(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	run := cluster.ZarfRuntimeMeta{
		Registries: []cluster.ZarfClusterRegistries{
			{
				Name:  "docker.io",
				Proxy: &cluster.ZarfClusterRegistryProxy{URL: "mirror.example.com:5000"},
				TLS:   &cluster.ZarfClusterRegistryTLS{CA: testCAPEM},
			},
			{
				Name: "nexus.example.com",
				Authentication: cluster.ZarfClusterRegistryAuth{
					Username: "robot",
					Password: "secretpassword",
				},
				TLS: &cluster.ZarfClusterRegistryTLS{
					CAFile:   "/etc/pki/ca-trust/source/anchors/nexus-ca.pem",
					CertFile: "/etc/ssl/certs/nexus-client.pem",
					KeyFile:  "/etc/ssl/private/nexus-client-key.pem",
				},
			},
			{
				Name:  "quay.io",
				Proxy: &cluster.ZarfClusterRegistryProxy{URL: "http://mirror-quay.example.com"},
				TLS:   &cluster.ZarfClusterRegistryTLS{InsecureSkipVerify: true},
			},
		},
	}
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.ZarfHost{Role: cluster.RoleWorker, Hostname: "node1"})

	if err := d.ConfigureEngine(context.Background(), host, run, dis); err != nil {
		t.Fatalf("ConfigureEngine() error = %v", err)
	}

	registriesPath := filepath.Join(filepath.Dir(d.Config), "registries.yaml")
	got, written := cfg.files[registriesPath]
	if !written {
		t.Fatalf("expected file %s to be written, files = %+v", registriesPath, cfg.files)
	}

	want, err := os.ReadFile("testdata/registries-tls.yaml")
	if err != nil {
		t.Fatalf("failed to read golden file: %v", err)
	}
	if got != string(want) {
		t.Errorf("registries.yaml = \n%s\nwant\n%s", got, want)
	}

	// The inline CA is written next to the file that references it, and the reference points at
	// the path it was written to.
	caPath := "/etc/cargoship/tls/mirror.example.com_5000.crt"
	if cfg.files[caPath] != testCAPEM {
		t.Errorf("%s = %q, want the inline CA certificate", caPath, cfg.files[caPath])
	}
}

func TestDesiredFilesWritesInlineCA(t *testing.T) {
	d := newTestRancher()
	run := cluster.ZarfRuntimeMeta{
		Registries: []cluster.ZarfClusterRegistries{
			{
				Name:  "docker.io",
				Proxy: &cluster.ZarfClusterRegistryProxy{URL: "mirror.example.com"},
				TLS:   &cluster.ZarfClusterRegistryTLS{CA: testCAPEM},
			},
		},
	}

	files, err := d.DesiredFiles(&cluster.ZarfHost{}, run, distro.ZarfDistro{})
	require.NoError(t, err)
	require.Equal(t, []byte(testCAPEM), files["/etc/cargoship/tls/mirror.example.com.crt"].Content)
	require.Contains(t, files, filepath.Join(filepath.Dir(d.Config), "registries.yaml"))
}

// registries.yaml is group readable, so anything running as the engine's group can read the
// mirror list. Every other file cargoship writes stays root-only.
func TestDesiredFilesModes(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	dis.Spec.Config.Engine = dig.Mapping{
		config.EngineAudit: dig.Mapping{"rules": []string{"foo"}},
		config.EnginePSS:   dig.Mapping{"defaults": dig.Mapping{"enforce": "restricted"}},
	}
	run := cluster.ZarfRuntimeMeta{
		Registries: []cluster.ZarfClusterRegistries{
			{
				Name:  "docker.io",
				Proxy: &cluster.ZarfClusterRegistryProxy{URL: "mirror.example.com"},
				TLS:   &cluster.ZarfClusterRegistryTLS{CA: testCAPEM},
			},
		},
	}

	files, err := d.DesiredFiles(&cluster.ZarfHost{}, run, dis)
	require.NoError(t, err)

	dir := filepath.Dir(d.Config)
	require.Equal(t, "0640", files[filepath.Join(dir, "registries.yaml")].Mode)
	require.Equal(t, "0600", files[filepath.Join(dir, "audit.yaml")].Mode)
	require.Equal(t, "0600", files[filepath.Join(dir, "pss.yaml")].Mode)
	require.Equal(t, "0600", files["/etc/cargoship/tls/mirror.example.com.crt"].Mode)
}

// A manifest entry written as a YAML mapping rather than a block string is Helm values all the
// same, so it lands in valuesContent as YAML, not as a Go-formatted map.
func TestConfigureEngineWritesManifestsFromMapping(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	dis.Spec.Config.Engine = dig.Mapping{
		config.EngineManifest: dig.Mapping{
			"my-chart": dig.Mapping{
				"kubeProxyReplacement": true,
				"k8sServicePort":       6443,
				"tolerations":          []any{dig.Mapping{"operator": "Exists"}},
			},
		},
	}
	run := cluster.ZarfRuntimeMeta{}
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.ZarfHost{Role: cluster.RoleController, Hostname: "node1"})
	host.Metadata.IsLeader = true

	require.NoError(t, d.ConfigureEngine(context.Background(), host, run, dis))

	path := filepath.Join(d.Data, "server/manifests/my-chart-config.yaml")
	got := parseWrittenYAML(t, cfg.files, path)

	values := dig.Mapping{}
	require.NoError(t, yaml.Unmarshal([]byte(got.DigString(keySpec, "valuesContent")), &values))
	require.Equal(t, true, values.Dig("kubeProxyReplacement"))
	require.Equal(t, 6443, values.Dig("k8sServicePort"))
	require.Equal(t, []any{dig.Mapping{"operator": "Exists"}}, values.Dig("tolerations"))
}

func TestHelmValuesContent(t *testing.T) {
	tests := map[string]struct {
		in   any
		want string
	}{
		"string passes through": {in: "foo: bar", want: "foo: bar"},
		"mapping is marshalled": {in: dig.Mapping{"foo": dig.Mapping{"bar": "baz"}}, want: "foo:\n  bar: baz\n"},
		"scalar is marshalled":  {in: 6443, want: "6443\n"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := helmValuesContent(tt.in)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// manifestEngine is an engine configuration whose manifest section carries both kinds of chart
// entry the section allows: a mapping cargoship marshals, and a YAML string it passes through.
func manifestEngine() dig.Mapping {
	return dig.Mapping{
		config.EngineManifest: dig.Mapping{
			"rke2-cilium": dig.Mapping{
				"encryption": dig.Mapping{"enabled": true},
			},
			"rancher-vsphere-cpi": "vCenter:\n  host: \"\"\n",
		},
	}
}

// The HelmChartConfig files are part of the desired set, so they are written, checked for drift,
// and pruned the way every other file cargoship puts on a host is.
func TestDesiredFilesHelmChartConfigs(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	dis.Spec.Config.Engine = manifestEngine()

	got, err := d.DesiredFiles(&cluster.ZarfHost{Role: cluster.RoleController}, cluster.ZarfRuntimeMeta{}, dis)
	require.NoError(t, err)

	manifests := filepath.Join(d.Data, "server", "manifests")
	ciliumPath := filepath.Join(manifests, "rke2-cilium-config.yaml")
	vspherePath := filepath.Join(manifests, "rancher-vsphere-cpi-config.yaml")
	require.Len(t, got, 2)
	require.Contains(t, got, ciliumPath)
	require.Contains(t, got, vspherePath)

	// The engine's helm controller reconciles these on its own, so a changed value does not
	// have to wait for a node to be drained and restarted.
	require.True(t, got[ciliumPath].NoRestart, "a chart config does not need the engine restarted")
	require.Equal(t, modeConfigFile, got[ciliumPath].Mode)

	var chartConfig dig.Mapping
	require.NoError(t, yaml.Unmarshal(got[ciliumPath].Content, &chartConfig))
	require.Equal(t, "HelmChartConfig", chartConfig["kind"])
	require.Equal(t, "helm.cattle.io/v1", chartConfig["apiVersion"])
	require.Equal(t, "rke2-cilium", chartConfig.DigMapping("metadata")["name"])
	require.Equal(t, "kube-system", chartConfig.DigMapping("metadata")["namespace"])
	require.Contains(t, chartConfig.DigString("spec", "valuesContent"), "enabled: true")

	// A string entry is the values file's own contents, and reaches the chart as written.
	var vsphereConfig dig.Mapping
	require.NoError(t, yaml.Unmarshal(got[vspherePath].Content, &vsphereConfig))
	require.Equal(t, "vCenter:\n  host: \"\"\n", vsphereConfig.DigString("spec", "valuesContent"))
}

// Only controllers read the manifest directory, so only controllers are given anything to put
// in it.
func TestDesiredFilesHelmChartConfigsControllerOnly(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	dis.Spec.Config.Engine = manifestEngine()

	got, err := d.DesiredFiles(&cluster.ZarfHost{Role: cluster.RoleWorker}, cluster.ZarfRuntimeMeta{}, dis)
	require.NoError(t, err)
	require.Empty(t, got, "an agent carries no chart configuration")
}

// A chart the engine was told not to install has nothing to configure, so cargoship writes no
// HelmChartConfig for it. Its neighbours are unaffected, which is what keeps a package that
// disables one bundled chart from losing the configuration of the rest.
func TestDesiredFilesHelmChartConfigsSkipsDisabledCharts(t *testing.T) {
	manifests := filepath.Join(newTestRancher().Data, "server", "manifests")
	ciliumPath := filepath.Join(manifests, "rke2-cilium-config.yaml")
	vspherePath := filepath.Join(manifests, "rancher-vsphere-cpi-config.yaml")

	tests := map[string]struct {
		disable any
		want    []string
	}{
		"a list is the shape a values mapping produces": {
			disable: []any{"rke2-cilium"},
			want:    []string{vspherePath},
		},
		"a typed list is the shape the generated config carries": {
			disable: []string{"rke2-cilium", "rancher-vsphere-cpi"},
		},
		"the engines also accept a single name written bare": {
			disable: "rke2-cilium",
			want:    []string{vspherePath},
		},
		"surrounding whitespace is not part of the name": {
			disable: []any{" rke2-cilium\n"},
			want:    []string{vspherePath},
		},
		"a name no chart is configured under does nothing": {
			disable: []any{"rke2-ingress-nginx"},
			want:    []string{ciliumPath, vspherePath},
		},
		"an empty entry disables nothing": {
			disable: []any{""},
			want:    []string{ciliumPath, vspherePath},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			d := newTestRancher()
			dis := distro.ZarfDistro{}
			dis.Spec.Config.Engine = manifestEngine()
			dis.Spec.Config.Engine[config.EngineConfig] = dig.Mapping{keyDisable: tt.disable}

			got, err := d.DesiredFiles(&cluster.ZarfHost{Role: cluster.RoleController}, cluster.ZarfRuntimeMeta{}, dis)
			require.NoError(t, err)

			paths := slices.Collect(maps.Keys(got))
			slices.Sort(paths)
			want := slices.Clone(tt.want)
			slices.Sort(want)
			require.Equal(t, want, paths)
		})
	}
}

// The disable key is the engine's own, so a package that never mentions it is written exactly as
// it was before disabling a chart was something values could ask for.
func TestDesiredFilesHelmChartConfigsWithoutDisable(t *testing.T) {
	d := newTestRancher()
	dis := distro.ZarfDistro{}
	dis.Spec.Config.Engine = manifestEngine()
	dis.Spec.Config.Engine[config.EngineConfig] = dig.Mapping{keyNodeLabel: []string{"role=worker"}}

	got, err := d.DesiredFiles(&cluster.ZarfHost{Role: cluster.RoleController}, cluster.ZarfRuntimeMeta{}, dis)
	require.NoError(t, err)
	require.Len(t, got, 2)
}
