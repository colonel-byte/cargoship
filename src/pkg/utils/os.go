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
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/types/os"
	"github.com/colonel-byte/cargoship/src/types/os/linux"
	"github.com/colonel-byte/cargoship/src/types/os/linux/enterpriselinux"
)

// FilterEngineAlreadyPopulated is a function used to filter whether a host already has distro files populated
func FilterEngineAlreadyPopulated(h *cluster.ZarfHost) bool {
	return !h.Metadata.EngineUploaded
}

// IsDebianLinux is true if the os.Configurer is of a type of Debian based OS
func IsDebianLinux(con os.Configurer) bool {
	switch con.(type) {
	case *linux.Debian, *linux.Ubuntu:
		return true
	default:
		return false
	}
}

// FilterDebianLinux is a function used to filter whether a host is a Debian based OS
func FilterDebianLinux(h *cluster.ZarfHost) bool {
	return IsDebianLinux(h.Configurer)
}

// IsEnterpriseLinux is true if the os.Configurer is of a type of Enterprise Linux
func IsEnterpriseLinux(con os.Configurer) bool {
	switch con.(type) {
	case *enterpriselinux.AlmaLinux, *enterpriselinux.AmazonLinux, *enterpriselinux.CentOS, *enterpriselinux.Fedora, *enterpriselinux.OracleLinux, *enterpriselinux.RHEL, *enterpriselinux.RockyLinux:
		return true
	default:
		return false
	}
}

// FilterEnterpriseLinux is a function used to filter whether a host is an Enterprise Linux OS
func FilterEnterpriseLinux(h *cluster.ZarfHost) bool {
	return IsEnterpriseLinux(h.Configurer)
}
