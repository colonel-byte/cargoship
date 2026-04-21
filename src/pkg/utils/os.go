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

package utils

import (
	"github.com/colonel-byte/mare/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/mare/src/types/os"
	"github.com/colonel-byte/mare/src/types/os/linux"
	"github.com/colonel-byte/mare/src/types/os/linux/enterpriselinux"
)

func IsDebianLinux(con os.Configurer) bool {
	switch con.(type) {
	case *linux.Debian, *linux.Ubuntu:
		return true
	default:
		return false
	}
}

func FilterDebianLinux(h *cluster.ZarfHost) bool {
	if h.Metadata.EngineUploaded {
		return false
	}
	return IsDebianLinux(h.Configurer)
}

func IsEnterpriseLinux(con os.Configurer) bool {
	switch con.(type) {
	case *enterpriselinux.AlmaLinux, *enterpriselinux.AmazonLinux, *enterpriselinux.CentOS, *enterpriselinux.Fedora, *enterpriselinux.OracleLinux, *enterpriselinux.RHEL, *enterpriselinux.RockyLinux:
		return true
	default:
		return false
	}
}

func FilterEnterpriseLinux(h *cluster.ZarfHost) bool {
	if h.Metadata.EngineUploaded {
		return false
	}
	return IsEnterpriseLinux(h.Configurer)
}
