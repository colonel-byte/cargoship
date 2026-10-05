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

package clustercfg

import (
	"context"
	"reflect"
	"testing"
)

func TestParseValues(t *testing.T) {
	inventory := []byte(`apiVersion: zarf.dev/v1alpha1
kind: ZarfCluster
metadata:
  name: test
spec:
  config:
    loadbalancer: lb.example.com
    values:
      cilium:
        enabled: true
        ipam:
          mode: kubernetes
        cidrs:
          - 10.42.0.0/16
  hosts:
    - hostname: node1
      role: controller
`)

	c, err := Parse(context.Background(), inventory)
	if err != nil {
		t.Fatal(err)
	}

	// Nested values have to decode as map[string]any, since that is what the
	// merge and the schema check both work on.
	want := map[string]any{
		"cilium": map[string]any{
			"enabled": true,
			"ipam":    map[string]any{"mode": "kubernetes"},
			"cidrs":   []any{"10.42.0.0/16"},
		},
	}
	if !reflect.DeepEqual(c.Spec.Config.Values, want) {
		t.Fatalf("Spec.Config.Values = %#v, want %#v", c.Spec.Config.Values, want)
	}
}

func TestParseNoValues(t *testing.T) {
	inventory := []byte(`apiVersion: zarf.dev/v1alpha1
kind: ZarfCluster
metadata:
  name: test
spec:
  config:
    loadbalancer: lb.example.com
  hosts:
    - hostname: node1
      role: controller
`)

	c, err := Parse(context.Background(), inventory)
	if err != nil {
		t.Fatal(err)
	}
	if c.Spec.Config.Values != nil {
		t.Fatalf("Spec.Config.Values = %#v, want nil", c.Spec.Config.Values)
	}
}
