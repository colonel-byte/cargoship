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

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
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

	got := d.KubeconfigPath(&cluster.ZarfHost{}, "/var/lib/kubelet")

	if got != "/etc/kubernetes/admin.conf" {
		t.Fatalf("KubeconfigPath() = %q, want %q", got, "/etc/kubernetes/admin.conf")
	}
}

func TestUpstreamKubectlCmdf(t *testing.T) {
	d := newTestUpstream()

	got := d.KubectlCmdf(&cluster.ZarfHost{}, "", "get nodes -o %s", "wide")

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
	host := cfg.attach(&cluster.ZarfHost{})

	_, err := d.RunningVersion(host)

	if !errors.Is(err, ErrVersionNotDetected) {
		t.Fatalf("RunningVersion() error = %v, want %v", err, ErrVersionNotDetected)
	}
}

func TestUpstreamRunningVersionExecNotConnected(t *testing.T) {
	d := newTestUpstream()
	cfg := &fakeHost{lookPathResult: "/usr/bin/kubelet"}
	host := cfg.attach(&cluster.ZarfHost{})

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
			dis := distro.ZarfDistro{}
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
	dis := distro.ZarfDistro{}
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
	host := cfg.attach(&cluster.ZarfHost{Hostname: "worker-1", Role: cluster.RoleWorker})

	if err := d.ConfigureEngine(context.Background(), host, cluster.ZarfRuntimeMeta{}, distro.ZarfDistro{}); err != nil {
		t.Fatalf("ConfigureEngine() error = %v, want nil", err)
	}

	for _, path := range []string{containerdConfigPath, crictlConfigPath, kubeadmConfigPath} {
		if _, ok := cfg.files[path]; !ok {
			t.Fatalf("ConfigureEngine() did not write %s, files = %+v", path, cfg.files)
		}
	}
}

func TestUpstreamDesiredFilesRendersContainerdAndCrictl(t *testing.T) {
	d := newTestUpstream()

	files, err := d.DesiredFiles(&cluster.ZarfHost{}, cluster.ZarfRuntimeMeta{}, distro.ZarfDistro{})

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
	run := cluster.ZarfRuntimeMeta{
		Registries: []cluster.ZarfClusterRegistries{
			{
				Name: "docker.io",
				Proxy: &cluster.ZarfClusterRegistryProxy{
					URL: "mirror.example.com",
				},
			},
		},
	}

	files, err := d.DesiredFiles(&cluster.ZarfHost{}, run, distro.ZarfDistro{})

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
		host    *cluster.ZarfHost
		wantHas []string
		wantNot []string
	}{
		{
			name:    "leader",
			host:    &cluster.ZarfHost{Hostname: "leader-1", Metadata: cluster.ZarfHostMetadata{IsLeader: true}},
			wantHas: []string{"kind: ClusterConfiguration", "kind: InitConfiguration", "kind: KubeletConfiguration"},
			wantNot: []string{"kind: JoinConfiguration"},
		},
		{
			name:    "joining controller",
			host:    &cluster.ZarfHost{Hostname: "controller-2", Role: cluster.RoleController},
			wantHas: []string{"kind: JoinConfiguration", "kind: KubeletConfiguration", "controlPlane"},
			wantNot: []string{"kind: ClusterConfiguration", "kind: InitConfiguration"},
		},
		{
			name:    "worker",
			host:    &cluster.ZarfHost{Hostname: "worker-1", Role: cluster.RoleWorker},
			wantHas: []string{"kind: JoinConfiguration", "kind: KubeletConfiguration"},
			wantNot: []string{"kind: ClusterConfiguration", "kind: InitConfiguration", "controlPlane"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newTestUpstream()

			files, err := d.DesiredFiles(tt.host, cluster.ZarfRuntimeMeta{}, distro.ZarfDistro{})

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
	dis := distro.ZarfDistro{}
	dis.Spec.Config.Manifests = []string{"/etc/kubernetes/manifests/cni.yaml"}

	got := d.ManifestPaths(dis)
	want := []string{"/etc/kubernetes/manifests/cni.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ManifestPaths() = %v, want %v", got, want)
	}
}

func TestUpstreamManifestPathsEmpty(t *testing.T) {
	d := newTestUpstream()

	if got := d.ManifestPaths(distro.ZarfDistro{}); len(got) != 0 {
		t.Fatalf("ManifestPaths() = %v, want empty", got)
	}
}

func TestUpstreamStopControllerService(t *testing.T) {
	d := newTestUpstream()
	cfg := &fakeHost{serviceRunning: true}
	host := cfg.attach(&cluster.ZarfHost{})

	if err := d.StopControllerService(host); err != nil {
		t.Fatalf("StopControllerService() error = %v", err)
	}
}

func TestUpstreamStopWorkerService(t *testing.T) {
	d := newTestUpstream()
	cfg := &fakeHost{serviceRunning: true}
	host := cfg.attach(&cluster.ZarfHost{})

	if err := d.StopWorkerService(host); err != nil {
		t.Fatalf("StopWorkerService() error = %v", err)
	}
}
