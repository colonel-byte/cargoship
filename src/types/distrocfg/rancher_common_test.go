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
	"reflect"
	"testing"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/k0sproject/dig"
)

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
