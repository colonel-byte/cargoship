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

	"github.com/k0sproject/dig"
	"gopkg.in/yaml.v3"
)

func main() {
	config := dig.Mapping{}
	config["apiVersion"] = "helm.cattle.io/v1"
	config["kind"] = "HelmChartConfig"
	config["metadata"] = map[string]string{
		"name":     "rke2-cilium",
		"namspace": "kube-system",
	}
	config["spec"] = map[string]string{
		"valuesContent": `This is a test`,
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(data))
}
