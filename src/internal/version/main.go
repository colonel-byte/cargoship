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
	"regexp"
	"sort"

	"github.com/Masterminds/semver/v3"
)

const (
	k3s = `k3s version v1.35.3+k3s1 (be38e884)
go version go1.25.7`
	rke2 = `rke2 version v1.35.2+rke2r1 (b1cd9d8e735bcd84cad7407109423a8dd7b648d8)
go version go1.25.7 X:boringcrypto`
)

func main() {
	raw := []string{"1.2.3", "1.0", "1.3", "2", "0.4.2", "1.35.3-rke2r2", "v1.35.3-rke2r1"}
	vs := make([]*semver.Version, len(raw))
	for i, r := range raw {
		v, err := semver.NewVersion(r)
		if err != nil {
			panic(err)
		}
		vs[i] = v
	}

	sort.Sort(semver.Collection(vs))

	for _, v := range vs {
		fmt.Println(v)
	}

	re := regexp.MustCompile(`v?[0-9]+\.[0-9]+\.[0-9]+\+[a-z0-9]+`)
	match := re.FindString(k3s)
	fmt.Println(match)
	match = re.FindString(rke2)
	fmt.Println(match)
}
