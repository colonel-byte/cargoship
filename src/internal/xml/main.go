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
	"encoding/xml"
	"fmt"
)

func main() {
	type Option struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	}
	type Person struct {
		XMLName xml.Name `xml:"ipset"`
		Type    string   `xml:"type,attr"`
		Short   string   `xml:"short"`
		Long    string   `xml:"description"`
		Option  Option   `xml:"option,omitempty"`
		Entries []string `xml:"entry"`
	}

	v := &Person{
		Type:  "hash:ip",
		Short: "k8nodes",
		Long:  "IPset for all k8 nodes",
		Entries: []string{
			"10.3.2.1",
			"10.3.2.2",
			"10.3.2.3",
		},
		Option: Option{
			Name:  "family",
			Value: "inet6",
		},
	}

	output, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Printf("error: %v\n", err)
	}

	fmt.Println(string(output))
}
