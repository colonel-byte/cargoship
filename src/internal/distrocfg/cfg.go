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

	v1alpha1 "github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/distro"
	goyaml "github.com/goccy/go-yaml"
)

// Parse parses the yaml passed as a byte slice and applies schema migrations.
func Parse(ctx context.Context, b []byte) (v1alpha1.ZarfDistro, error) {
	var dis v1alpha1.ZarfDistro
	err := goyaml.Unmarshal(b, &dis)
	if err != nil {
		return v1alpha1.ZarfDistro{}, err
	}
	return dis, nil
}
