// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dnfpins_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/magefiles/pkg/devtools/dnfpins"
)

func TestRpmVersionCompare(t *testing.T) {
	v1 := dnfpins.RpmVersion{Epoch: "1", Ver: "2.16.16", Rel: "2.el10"}
	v2 := dnfpins.RpmVersion{Epoch: "1", Ver: "2.16.16", Rel: "2.el10_2.1"}
	v3 := dnfpins.RpmVersion{Epoch: "2", Ver: "1.0", Rel: "1"}

	if v1.Compare(v2) >= 0 {
		t.Errorf("expected v1 < v2")
	}
	if v2.Compare(v1) <= 0 {
		t.Errorf("expected v2 > v1")
	}
	if v2.Compare(v3) >= 0 {
		t.Errorf("expected v2 < v3 due to epoch")
	}
}

func TestUpdateDockerfileAnsiblePins(t *testing.T) {
	tmpDir := t.TempDir()
	dfPath := filepath.Join(tmpDir, "Dockerfile")

	initial := `FROM ghcr.io/almalinux/10-base:10.2
ARG TARGETPLATFORM
ARG ANSIBLE_CORE_VERSION="old"
ARG BASH_COMPLETION_VERSION="old"

RUN dnf install -y ansible-core-${ANSIBLE_CORE_VERSION}
`
	if err := os.WriteFile(dfPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := dnfpins.UpdateDockerfileAnsiblePins(dfPath, "2.16.16-2.el10_2.1", "2.11-16.el10"); err != nil {
		t.Fatalf("UpdateDockerfileAnsiblePins failed: %v", err)
	}

	data, err := os.ReadFile(dfPath)
	if err != nil {
		t.Fatal(err)
	}

	expected := `FROM ghcr.io/almalinux/10-base:10.2
ARG TARGETPLATFORM
ARG ANSIBLE_CORE_VERSION="2.16.16-2.el10_2.1"
ARG BASH_COMPLETION_VERSION="2.11-16.el10"

RUN dnf install -y ansible-core-${ANSIBLE_CORE_VERSION}
`
	if string(data) != expected {
		t.Errorf("content mismatch:\ngot:\n%s\nwant:\n%s", string(data), expected)
	}
}

func TestUpdateDockerfileUbiPins(t *testing.T) {
	tmpDir := t.TempDir()
	dfPath := filepath.Join(tmpDir, "Dockerfile")

	initial := `FROM ghcr.io/almalinux/10-minimal:10.2
ARG TARGETPLATFORM
ARG SHADOW_UTILS_VERSION="old"
ARG BASH_COMPLETION_VERSION="old"

RUN microdnf install -y shadow-utils-${SHADOW_UTILS_VERSION}
`
	if err := os.WriteFile(dfPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := dnfpins.UpdateDockerfileUbiPins(dfPath, "4.15.0-11.el10", "2.11-16.el10"); err != nil {
		t.Fatalf("UpdateDockerfileUbiPins failed: %v", err)
	}

	data, err := os.ReadFile(dfPath)
	if err != nil {
		t.Fatal(err)
	}

	expected := `FROM ghcr.io/almalinux/10-minimal:10.2
ARG TARGETPLATFORM
ARG SHADOW_UTILS_VERSION="4.15.0-11.el10"
ARG BASH_COMPLETION_VERSION="2.11-16.el10"

RUN microdnf install -y shadow-utils-${SHADOW_UTILS_VERSION}
`
	if string(data) != expected {
		t.Errorf("content mismatch:\ngot:\n%s\nwant:\n%s", string(data), expected)
	}
}

func TestUpdateGoreleaserDnfPins(t *testing.T) {
	tmpDir := t.TempDir()
	grPath := filepath.Join(tmpDir, ".goreleaser.yaml")

	initial := `dockers_v2:
  - id: cargoship-ubi
    build_args:
      SHADOW_UTILS_VERSION: old
      BASH_COMPLETION_VERSION: old
  - id: cargoship-ansible
    build_args:
      ANSIBLE_CORE_VERSION: old
      BASH_COMPLETION_VERSION: old
`
	if err := os.WriteFile(grPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	pins := &dnfpins.AlmaLinuxPins{
		AnsibleCore:    "2.16.16-2.el10_2.1",
		BashCompletion: "2.11-16.el10",
		ShadowUtils:    "4.15.0-11.el10",
	}

	if err := dnfpins.UpdateGoreleaserDnfPins(grPath, pins); err != nil {
		t.Fatalf("UpdateGoreleaserDnfPins failed: %v", err)
	}

	data, err := os.ReadFile(grPath)
	if err != nil {
		t.Fatal(err)
	}

	expected := `dockers_v2:
  - id: cargoship-ubi
    build_args:
      SHADOW_UTILS_VERSION: 4.15.0-11.el10
      BASH_COMPLETION_VERSION: 2.11-16.el10
  - id: cargoship-ansible
    build_args:
      ANSIBLE_CORE_VERSION: 2.16.16-2.el10_2.1
      BASH_COMPLETION_VERSION: 2.11-16.el10
`
	if string(data) != expected {
		t.Errorf("content mismatch:\ngot:\n%s\nwant:\n%s", string(data), expected)
	}
}
