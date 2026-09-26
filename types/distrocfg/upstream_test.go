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
				"config": dig.Mapping{keyServiceSubnet: "172.16.0.0/16"},
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
			keyPodSubnet:     "10.10.0.0/16",
			keyServiceSubnet: "10.20.0.0/16",
		},
	}

	got := d.GetClusterCIDR(dis)
	want := []string{"10.20.0.0/16", "10.10.0.0/16"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetClusterCIDR() = %v, want %v", got, want)
	}
}

func TestUpstreamConfigureEngineNotImplemented(t *testing.T) {
	d := newTestUpstream()

	err := d.ConfigureEngine(context.Background(), &cluster.ZarfHost{}, cluster.ZarfRuntimeMeta{}, distro.ZarfDistro{})

	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("ConfigureEngine() error = %v, want %v", err, ErrNotImplemented)
	}
}

func TestUpstreamDesiredFilesEmpty(t *testing.T) {
	d := newTestUpstream()

	files, err := d.DesiredFiles(&cluster.ZarfHost{}, cluster.ZarfRuntimeMeta{}, distro.ZarfDistro{})

	if err != nil {
		t.Fatalf("DesiredFiles() error = %v, want nil", err)
	}
	if len(files) != 0 {
		t.Fatalf("DesiredFiles() = %v, want empty", files)
	}
}

func TestUpstreamManagedDirsEmpty(t *testing.T) {
	d := newTestUpstream()

	if got := d.ManagedDirs(); got != nil {
		t.Fatalf("ManagedDirs() = %v, want nil", got)
	}
}

func TestUpstreamCleanupPaths(t *testing.T) {
	d := newTestUpstream()

	got := d.CleanupPaths()
	want := []string{"/var/lib/kubelet", "/etc/kubernetes", "/var/lib/kubernetes"}
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
