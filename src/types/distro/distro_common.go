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

package distro

import "errors"

type Common struct {
	//keep-sorted start
	Binary             string
	BinaryDir          string
	Config             string
	Data               string
	ID                 string
	Service_Controller string
	Service_Worker     string
	Token              string
	//keep-sorted end
}

var ErrPathKey = errors.New("key for set path does not exist")

func (r *Common) BinaryPath() string {
	return r.BinaryDir + "/" + r.Binary
}

func (r *Common) BinaryName() string {
	return r.Binary
}

func (r *Common) ConfigPath() string {
	return r.Config
}

func (r *Common) JoinTokenPath() string {
	return r.Token
}

func (r *Common) DataDirDefaultPath() string {
	return r.Data
}

func (r *Common) GetWorkerService() string {
	return r.Service_Worker
}

func (r *Common) GetControllerService() string {
	return r.Service_Controller
}

func (r *Common) SetPath(key string, value string) error {
	switch key {
	case Binary:
		r.Binary = value
	case BinaryDir:
		r.BinaryDir = value
	case Config:
		r.Config = value
	case Token:
		r.Token = value
	case Data:
		r.Data = value
	default:
		return ErrPathKey
	}
	return nil
}
