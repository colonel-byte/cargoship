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

// Package clustercfg is used to parse an byte array and returns a ZarfCluster
package clustercfg

import (
	"context"
	"fmt"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	goyaml "github.com/goccy/go-yaml"
)

// Parse parses the yaml passed as a byte slice and applies schema migrations.
func Parse(_ context.Context, b []byte) (_ cluster.ZarfCluster, err error) {
	// A bare "!" tag with no following value (e.g. "registries: ! ") makes go-yaml's decoder call
	// ArrayRange on a TagNode whose Value isn't an array, which returns a nil *ArrayNodeIter that
	// the decoder then dereferences -- a panic rather than a decode error. Recovering keeps a
	// malformed cluster config file the parse error it is, rather than a crash on arbitrary input.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("parse cluster config: %v", r)
		}
	}()

	var dis cluster.ZarfCluster
	if err := goyaml.Unmarshal(b, &dis); err != nil {
		return cluster.ZarfCluster{}, err
	}
	return dis, nil
}
