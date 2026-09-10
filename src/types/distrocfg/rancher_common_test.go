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
	"errors"
	"fmt"
	iofs "io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
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

func TestBuildRegistriesConfigNoAuth(t *testing.T) {
	registries := []cluster.ZarfClusterRegistries{
		{
			Name: "docker.io",
			Proxy: cluster.ZarfClusterRegistryProxy{
				URL: "mirror-docker-hub.example.com",
			},
		},
	}

	got := buildRegistriesConfig(registries)

	want := dig.Mapping{
		keyMirrors: dig.Mapping{
			"docker.io": dig.Mapping{
				keyEndpoint: []string{"mirror-docker-hub.example.com"},
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
			Proxy: cluster.ZarfClusterRegistryProxy{
				URL: "mirror-ghcr.example.com",
			},
			Authentication: cluster.ZarfClusterRegistryAuth{
				Username: "user",
				Password: "pass",
				Token:    "tok",
			},
		},
	}

	got := buildRegistriesConfig(registries)

	want := dig.Mapping{
		keyMirrors: dig.Mapping{
			"ghcr.io": dig.Mapping{
				keyEndpoint: []string{"mirror-ghcr.example.com"},
			},
		},
		keyConfigs: dig.Mapping{
			"mirror-ghcr.example.com": dig.Mapping{
				keyAuth: dig.Mapping{
					keyUsername:      "user",
					keyPassword:      "pass",
					keyIdentityToken: "tok",
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRegistriesConfig() = %+v, want %+v", got, want)
	}
}

func TestBuildRegistriesConfigWithRewrite(t *testing.T) {
	registries := []cluster.ZarfClusterRegistries{
		{
			Name: "docker.io",
			Proxy: cluster.ZarfClusterRegistryProxy{
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
				keyEndpoint: []string{"mirror-docker-hub.example.com"},
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
			Proxy: cluster.ZarfClusterRegistryProxy{URL: "mirror-docker-hub.example.com"},
		},
		{
			Name:  "quay.io",
			Proxy: cluster.ZarfClusterRegistryProxy{URL: "mirror-quay.example.com"},
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

	want := dig.Mapping{
		keyMirrors: dig.Mapping{},
	}
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
	if got.DigString(keyToken) != d.JoinTokenPath() {
		t.Errorf("config.yaml token-file = %q, want %q", got.DigString(keyToken), d.JoinTokenPath())
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
	if got.DigString(keyToken) != d.JoinTokenPathAgent() {
		t.Errorf("config.yaml token-file = %q, want %q", got.DigString(keyToken), d.JoinTokenPathAgent())
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
				Proxy: cluster.ZarfClusterRegistryProxy{URL: "mirror.example.com"},
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
	if !ok || len(endpoints) != 1 || endpoints[0] != "mirror.example.com" {
		t.Errorf("registries.yaml mirrors[docker.io].endpoint = %+v, want [mirror.example.com]", docker[keyEndpoint])
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
				Proxy: cluster.ZarfClusterRegistryProxy{
					URL: "mirror-docker-hub.example.com",
					Rewrite: map[string]string{
						"^rancher/(.*)": "mirrorproject/rancher-images/$1",
					},
				},
			},
			{
				Name:  "ghcr.io",
				Proxy: cluster.ZarfClusterRegistryProxy{URL: "mirror-ghcr.example.com"},
				Authentication: cluster.ZarfClusterRegistryAuth{
					Username: "user",
					Password: "pass",
					Token:    "tok",
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
				Proxy: cluster.ZarfClusterRegistryProxy{URL: "mirror.example.com"},
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
	if err := yaml.Unmarshal(got[auditPath], &auditYAML); err != nil {
		t.Fatalf("failed to unmarshal audit.yaml: %v", err)
	}
	if auditYAML.DigString(keyKind) != "Policy" || auditYAML.DigString(keyAPIVersion) != "audit.k8s.io/v1" {
		t.Errorf("audit.yaml = %+v, want kind=Policy apiVersion=audit.k8s.io/v1", auditYAML)
	}

	if err := yaml.Unmarshal(got[pssPath], &pssYAML); err != nil {
		t.Fatalf("failed to unmarshal pss.yaml: %v", err)
	}
	if pssYAML.DigString(keyKind) != "AdmissionConfiguration" || pssYAML.DigString(keyAPIVersion) != "apiserver.config.k8s.io/v1" {
		t.Errorf("pss.yaml = %+v, want kind=AdmissionConfiguration apiVersion=apiserver.config.k8s.io/v1", pssYAML)
	}

	wantRegistries, err := marshalYAML(buildRegistriesConfig(run.Registries))
	if err != nil {
		t.Fatalf("marshalYAML() error = %v", err)
	}
	if string(got[registriesPath]) != string(wantRegistries) {
		t.Errorf("registries.yaml = %s, want %s", got[registriesPath], wantRegistries)
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
