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
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/k0sproject/dig"
)

func newTestUpstream() *Upstream {
	return &Upstream{
		Common{
			ID:                DistroUpstream,
			BinaryDir:         "/usr/bin",
			Binary:            "kubectl",
			Config:            "/etc/kubernetes",
			Data:              "/var/lib/kubelet",
			PackageDir:        "/var/lib/kubernetes",
			ServiceController: "kubelet",
			ServiceWorker:     "kubelet",
		},
	}
}

func TestUpstreamKubeconfigPath(t *testing.T) {
	d := newTestUpstream()

	got := d.KubeconfigPath(&cluster.Host{}, "/var/lib/kubelet")

	if got != "/etc/kubernetes/admin.conf" {
		t.Fatalf("KubeconfigPath() = %q, want %q", got, "/etc/kubernetes/admin.conf")
	}
}

func TestUpstreamKubectlCmdf(t *testing.T) {
	d := newTestUpstream()

	got := d.KubectlCmdf(&cluster.Host{}, "", "get nodes -o %s", "wide")

	want := `env "KUBECONFIG=/etc/kubernetes/admin.conf" kubectl get nodes -o wide`
	if got != want {
		t.Fatalf("KubectlCmdf() = %q, want %q", got, want)
	}
}

func TestUpstreamDistroCmdf(t *testing.T) {
	d := newTestUpstream()

	got := d.DistroCmdf("token create --ttl %s", "10m")

	want := "token create --ttl 10m"
	if got != want {
		t.Fatalf("DistroCmdf() = %q, want %q", got, want)
	}
}

func TestUpstreamRunningVersionLookPathError(t *testing.T) {
	d := newTestUpstream()
	cfg := &fakeHost{lookPathErr: errors.New("not found")}
	host := cfg.attach(&cluster.Host{})

	_, err := d.RunningVersion(host)

	if !errors.Is(err, ErrVersionNotDetected) {
		t.Fatalf("RunningVersion() error = %v, want %v", err, ErrVersionNotDetected)
	}
}

func TestUpstreamRunningVersionExecNotConnected(t *testing.T) {
	d := newTestUpstream()
	cfg := &fakeHost{lookPathResult: "/usr/bin/kubelet"}
	host := cfg.attach(&cluster.Host{})

	_, err := d.RunningVersion(host)

	if !errors.Is(err, ErrVersionNotDetected) {
		t.Fatalf("RunningVersion() error = %v, want %v", err, ErrVersionNotDetected)
	}
}

func TestUpstreamGetClusterCIDR(t *testing.T) {
	tests := []struct {
		name   string
		engine dig.Mapping
		want   []string
	}{
		{
			name:   "no engine config set",
			engine: nil,
			want:   []string{upstreamServiceCIDR},
		},
		{
			name: "service subnet overridden, no pod subnet",
			engine: dig.Mapping{
				"config": dig.Mapping{
					keyNetworking: dig.Mapping{keyServiceSubnet: "172.16.0.0/16"},
				},
			},
			want: []string{"172.16.0.0/16"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newTestUpstream()
			dis := distro.Distro{}
			dis.Spec.Config.Engine = tt.engine

			got := d.GetClusterCIDR(dis)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("GetClusterCIDR() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUpstreamGetClusterCIDRWithPodSubnet(t *testing.T) {
	d := newTestUpstream()
	dis := distro.Distro{}
	dis.Spec.Config.Engine = dig.Mapping{
		"config": dig.Mapping{
			keyNetworking: dig.Mapping{
				keyPodSubnet:     "10.10.0.0/16",
				keyServiceSubnet: "10.20.0.0/16",
			},
		},
	}

	got := d.GetClusterCIDR(dis)
	want := []string{"10.20.0.0/16", "10.10.0.0/16"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetClusterCIDR() = %v, want %v", got, want)
	}
}

func TestUpstreamConfigureEngineWritesDesiredFiles(t *testing.T) {
	d := newTestUpstream()
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.Host{Hostname: "worker-1", Role: cluster.RoleWorker})

	if err := d.ConfigureEngine(context.Background(), host, cluster.RuntimeMeta{}, distro.Distro{}); err != nil {
		t.Fatalf("ConfigureEngine() error = %v, want nil", err)
	}

	for _, path := range []string{containerdConfigPath, crictlConfigPath, kubeadmConfigPath} {
		if _, ok := cfg.files[path]; !ok {
			t.Fatalf("ConfigureEngine() did not write %s, files = %+v", path, cfg.files)
		}
	}

	// The runtime is not touched here. The packages that provide it are installed by the
	// Initialize phases, after this one, so there is no containerd.service to enable yet -- the
	// fake reports an unexpected service call as an error, which is what would have surfaced it.
	if len(cfg.enabled)+len(cfg.started)+len(cfg.restarted) != 0 {
		t.Fatalf("ConfigureEngine() touched services: enabled = %v, started = %v, restarted = %v",
			cfg.enabled, cfg.started, cfg.restarted)
	}
}

// TestEnsureRuntimeBringsContainerdUp is the gap that stopped an upstream apply dead: cargoship
// installed containerd from the package and wrote its configuration, then ran `kubeadm init`
// against a runtime nothing had started, which failed its own preflight on the CRI socket.
//
// Unlike rke2 and k3s, whose agent embeds a runtime and starts it, this one is cargoship's to
// bring up. Bootstrap calls this, because that is the first point at which the package providing
// the service is installed.
func TestEnsureRuntimeBringsContainerdUp(t *testing.T) {
	d := newTestUpstream()

	stopped := &fakeHost{allowServiceChanges: true}
	host := stopped.attach(&cluster.Host{Hostname: "control-1", Role: cluster.RoleController})
	if err := d.ensureRuntime(context.Background(), host); err != nil {
		t.Fatalf("ensureRuntime() error = %v, want nil", err)
	}
	if want := []string{containerdService}; !reflect.DeepEqual(stopped.enabled, want) {
		t.Fatalf("enabled = %v, want %v: a node that reboots comes back without a runtime", stopped.enabled, want)
	}
	if want := []string{containerdService}; !reflect.DeepEqual(stopped.started, want) {
		t.Fatalf("started = %v, want %v", stopped.started, want)
	}

	// A runtime that is already up is enabled and left alone: restarting it here would bounce
	// the runtime under a kubelet on every host that rejoins.
	running := &fakeHost{
		serviceRunning:      true,
		allowServiceChanges: true,
	}
	if err := d.ensureRuntime(context.Background(), running.attach(&cluster.Host{Hostname: "worker-1"})); err != nil {
		t.Fatalf("ensureRuntime() error = %v, want nil", err)
	}
	if want := []string{containerdService}; !reflect.DeepEqual(running.enabled, want) {
		t.Fatalf("enabled = %v, want %v", running.enabled, want)
	}
	if len(running.started)+len(running.restarted) != 0 {
		t.Fatalf("started = %v, restarted = %v, want neither for a runtime already up",
			running.started, running.restarted)
	}
}

func TestUpstreamDesiredFilesRendersContainerdAndCrictl(t *testing.T) {
	d := newTestUpstream()

	files, err := d.DesiredFiles(context.Background(), &cluster.Host{}, cluster.RuntimeMeta{}, distro.Distro{})

	if err != nil {
		t.Fatalf("DesiredFiles() error = %v, want nil", err)
	}
	if _, ok := files[containerdConfigPath]; !ok {
		t.Fatalf("DesiredFiles() = %v, want a %s entry", files, containerdConfigPath)
	}
	if _, ok := files[crictlConfigPath]; !ok {
		t.Fatalf("DesiredFiles() = %v, want a %s entry", files, crictlConfigPath)
	}
}

func TestUpstreamDesiredFilesRendersRegistryHostsTOML(t *testing.T) {
	d := newTestUpstream()
	run := cluster.RuntimeMeta{
		Registries: []cluster.Registries{
			{
				Name: "docker.io",
				Proxy: &cluster.RegistryProxy{
					URL: "mirror.example.com",
				},
			},
		},
	}

	files, err := d.DesiredFiles(context.Background(), &cluster.Host{}, run, distro.Distro{})

	if err != nil {
		t.Fatalf("DesiredFiles() error = %v, want nil", err)
	}
	want := containerdHostsPath("docker.io")
	if _, ok := files[want]; !ok {
		t.Fatalf("DesiredFiles() = %v, want a %s entry", files, want)
	}
}

func TestUpstreamDesiredFilesRendersKubeadmConfigPerRole(t *testing.T) {
	tests := []struct {
		name    string
		host    *cluster.Host
		wantHas []string
		wantNot []string
	}{
		{
			name:    "leader",
			host:    &cluster.Host{Hostname: "leader-1", Metadata: cluster.HostMetadata{IsLeader: true}},
			wantHas: []string{"kind: ClusterConfiguration", "kind: InitConfiguration", "kind: KubeletConfiguration"},
			wantNot: []string{"kind: JoinConfiguration"},
		},
		{
			name:    "joining controller",
			host:    &cluster.Host{Hostname: "controller-2", Role: cluster.RoleController},
			wantHas: []string{"kind: JoinConfiguration", "kind: KubeletConfiguration", "controlPlane"},
			wantNot: []string{"kind: ClusterConfiguration", "kind: InitConfiguration"},
		},
		{
			name:    "worker",
			host:    &cluster.Host{Hostname: "worker-1", Role: cluster.RoleWorker},
			wantHas: []string{"kind: JoinConfiguration", "kind: KubeletConfiguration"},
			wantNot: []string{"kind: ClusterConfiguration", "kind: InitConfiguration", "controlPlane"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newTestUpstream()

			files, err := d.DesiredFiles(context.Background(), tt.host, cluster.RuntimeMeta{}, distro.Distro{})

			if err != nil {
				t.Fatalf("DesiredFiles() error = %v, want nil", err)
			}
			df, ok := files[kubeadmConfigPath]
			if !ok {
				t.Fatalf("DesiredFiles() = %v, want a %s entry", files, kubeadmConfigPath)
			}
			content := string(df.Content)
			for _, want := range tt.wantHas {
				if !strings.Contains(content, want) {
					t.Fatalf("kubeadm config = %q, want it to contain %q", content, want)
				}
			}
			for _, notWant := range tt.wantNot {
				if strings.Contains(content, notWant) {
					t.Fatalf("kubeadm config = %q, want it to not contain %q", content, notWant)
				}
			}
		})
	}
}

func TestUpstreamManagedDirsIncludesRegistryTLSDir(t *testing.T) {
	d := newTestUpstream()

	got := d.ManagedDirs()
	want := []ManagedDir{{Path: registryTLSDir}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ManagedDirs() = %v, want %v", got, want)
	}
}

func TestUpstreamCleanupPaths(t *testing.T) {
	d := newTestUpstream()

	got := d.CleanupPaths()
	want := []string{"/var/lib/kubelet", "/etc/kubernetes", "/var/lib/kubernetes", "/etc/cni/net.d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CleanupPaths() = %v, want %v", got, want)
	}
}

func TestUpstreamPackageStagingDir(t *testing.T) {
	d := newTestUpstream()

	if got := d.PackageStagingDir(); got != "/var/lib/kubernetes" {
		t.Fatalf("PackageStagingDir() = %q, want %q", got, "/var/lib/kubernetes")
	}
}

func TestUpstreamJoinTokenPathAgentEmpty(t *testing.T) {
	d := newTestUpstream()

	if got := d.JoinTokenPathAgent(); got != "" {
		t.Fatalf("JoinTokenPathAgent() = %q, want empty", got)
	}
}

func TestUpstreamManifestPaths(t *testing.T) {
	d := newTestUpstream()
	dis := distro.Distro{}
	dis.Spec.Config.Manifests = []string{"/etc/kubernetes/manifests/cni.yaml"}

	got := d.ManifestPaths(dis)
	want := []string{"/etc/kubernetes/manifests/cni.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ManifestPaths() = %v, want %v", got, want)
	}
}

func TestUpstreamManifestPathsEmpty(t *testing.T) {
	d := newTestUpstream()

	if got := d.ManifestPaths(distro.Distro{}); len(got) != 0 {
		t.Fatalf("ManifestPaths() = %v, want empty", got)
	}
}

func TestUpstreamStopControllerService(t *testing.T) {
	d := newTestUpstream()
	cfg := &fakeHost{serviceRunning: true}
	host := cfg.attach(&cluster.Host{})

	if err := d.StopControllerService(host); err != nil {
		t.Fatalf("StopControllerService() error = %v", err)
	}
}

func TestUpstreamStopWorkerService(t *testing.T) {
	d := newTestUpstream()
	cfg := &fakeHost{serviceRunning: true}
	host := cfg.attach(&cluster.Host{})

	if err := d.StopWorkerService(host); err != nil {
		t.Fatalf("StopWorkerService() error = %v", err)
	}
}

// TestUpstreamStopServiceToleratesAHostWithNoUnit is the reset an operator reaches for after an
// apply failed half way.
//
// A worker whose packages were staged but never installed has no kubelet unit, and systemd exits
// 5 with "Unit kubelet.service not loaded" for one, which failed the whole reset before the
// uninstall could remove the files that *are* there. Both roles share one kubelet service here,
// so both stops take the same guard.
func TestUpstreamStopServiceToleratesAHostWithNoUnit(t *testing.T) {
	d := newTestUpstream()

	// Nothing running: the service was never installed, so there is nothing to stop and the
	// fake would report an unexpected stop if one were attempted.
	absent := &fakeHost{}
	host := absent.attach(&cluster.Host{Hostname: "worker-1", Role: cluster.RoleWorker})
	if err := d.StopWorkerService(host); err != nil {
		t.Fatalf("StopWorkerService() error = %v, want nil for a host with no unit", err)
	}
	if err := d.StopControllerService(host); err != nil {
		t.Fatalf("StopControllerService() error = %v, want nil for a host with no unit", err)
	}
	if len(absent.stopped) != 0 {
		t.Fatalf("stopped = %v, want nothing attempted", absent.stopped)
	}

	// A running service is still stopped, which is the whole point of the call.
	running := &fakeHost{
		serviceRunning:      true,
		allowServiceChanges: true,
	}
	if err := d.StopWorkerService(running.attach(&cluster.Host{Hostname: "worker-1"})); err != nil {
		t.Fatalf("StopWorkerService() error = %v, want nil", err)
	}
	if want := []string{"kubelet"}; !reflect.DeepEqual(running.stopped, want) {
		t.Fatalf("stopped = %v, want %v", running.stopped, want)
	}
}
