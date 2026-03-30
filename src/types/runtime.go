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

package types

type DistroConfig struct {
	CreateOpts DistroCreateOptions
	DeployOpts DistroDeployOptions
}

type DistroCreateOptions struct {
	SourceDirectory string
	Output          string
	Version         string
	Name            string
	CachePath       string
}

type DistroDeployOptions struct {
	Source          string
	Config          string
	Packages        []string
	ForceConflicts  bool
	SetVariables    map[string]string                 `json:"setVariables" jsonschema:"description=Key-Value map of variable names and their corresponding values that will be used by Zarf packages in a bundle"`
	Variables       map[string]map[string]interface{} `yaml:"variables,omitempty"`
	SharedVariables map[string]interface{}            `yaml:"shared,omitempty"`
	Retries         int                               `yaml:"retries"`
	Options         map[string]interface{}            `yaml:"options,omitempty"`
}
