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

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/k0sproject/dig"
	"gopkg.in/yaml.v2"
)

const (
	test = "images/cluster.yaml"
)

func main() {
	path, err := filepath.Abs(test)
	if err != nil {
		panic(err)
	}
	file, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		panic(err)
	}

	test1(fileBytes)
}

func test1(yamlDoc []byte) {
	m := dig.Mapping{}

	if err := yaml.Unmarshal(yamlDoc, &m); err != nil {
		panic(err)
	}

	// prettyJSON, _ := json.MarshalIndent(m.Dig("spec", "config", "profiles"), "", "  ")

	// fmt.Println(string(prettyJSON))

	pro01 := m.Dig("spec", "config", "profiles")

	var profiles []cluster.ZarfClusterProfiles

	switch pro01 := pro01.(type) {
	case []any:
		for _, v := range pro01 {
			if s, ok := v.(cluster.ZarfClusterProfiles); ok {
				profiles = append(profiles, s)
			}
		}
	default:
		fmt.Println(pro01)
	}

	fmt.Println(len(profiles))
}
