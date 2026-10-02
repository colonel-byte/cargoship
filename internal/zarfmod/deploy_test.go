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

package zarfmod

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// deployFlagList is the recorded zarf package deploy flag surface.
const deployFlagList = "testdata/zarf-package-deploy-flags.txt"

func TestBuildDeployArgs(t *testing.T) {
	yes := true
	no := false

	tests := []struct {
		name   string
		params deployParams
		want   []string
	}{
		{
			name: "an oci reference is passed through unchanged",
			params: deployParams{
				Package: "oci://ghcr.io/colonel-byte/zarf/csi-local-path-provider:0.0.37-upstream",
			},
			want: []string{
				"package", "deploy",
				"oci://ghcr.io/colonel-byte/zarf/csi-local-path-provider:0.0.37-upstream",
				"--confirm",
				"--no-color",
				"--log-format", "json",
			},
		},
		{
			name: "components and signature verification",
			params: deployParams{
				Package:    "/srv/staging/csi-local-path.tar.zst",
				Components: "local-path-images,local-path-chart",
				PublicKey:  "/etc/zarf/packages.pub",
				Verify:     "always",
				Timeout:    "15m",
			},
			want: []string{
				"package", "deploy",
				"/srv/staging/csi-local-path.tar.zst",
				"--confirm",
				"--no-color",
				"--log-format", "json",
				"--components", "local-path-images,local-path-chart",
				"--timeout", "15m",
				"--key", "/etc/zarf/packages.pub",
				"--verify=always",
			},
		},
		{
			name: "a false boolean renders, an unset one does not",
			params: deployParams{
				Package:   "/srv/staging/pkg.tar.zst",
				Connected: &yes,
				PlainHTTP: &no,
			},
			want: []string{
				"package", "deploy",
				"/srv/staging/pkg.tar.zst",
				"--confirm",
				"--no-color",
				"--log-format", "json",
				"--connected=true",
				"--plain-http=false",
			},
		},
		{
			name: "variables render one flag per entry, sorted",
			params: deployParams{
				Package: "/srv/staging/pkg.tar.zst",
				SetVariables: map[string]string{
					"STORAGE_CLASS": "local-path",
					"NODE_PATH":     "/var/lib/local-path-provisioner",
				},
			},
			want: []string{
				"package", "deploy",
				"/srv/staging/pkg.tar.zst",
				"--confirm",
				"--no-color",
				"--log-format", "json",
				"--set-variables", "NODE_PATH=/var/lib/local-path-provisioner",
				"--set-variables", "STORAGE_CLASS=local-path",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDeployArgs(&tt.params)
			if strings.Join(got, " ") != strings.Join(tt.want, " ") {
				t.Errorf("buildDeployArgs rendered\n  %v\nwant\n  %v", got, tt.want)
			}
		})
	}
}

// TestDeployPackageResolvesALocalPath proves the one case where the module rewrites what the
// operator wrote: a relative path with no working directory named, where the directory the module
// inherits is Ansible's rather than the operator's.
func TestDeployPackageResolvesALocalPath(t *testing.T) {
	absolute := deployPackage(&deployParams{Package: "pkg.tar.zst"})
	if !filepath.IsAbs(absolute) {
		t.Errorf("a relative package with no directory was left relative: %q", absolute)
	}

	kept := deployPackage(&deployParams{
		Package:   "pkg.tar.zst",
		Directory: "/srv/staging",
	})
	if kept != "pkg.tar.zst" {
		t.Errorf("a relative package with a directory was rewritten to %q", kept)
	}

	remote := "oci://ghcr.io/colonel-byte/zarf/init/slim:v0.82.0-upstream"
	if got := deployPackage(&deployParams{Package: remote}); got != remote {
		t.Errorf("a remote reference was rewritten to %q", got)
	}
}

func TestDeployArgsAreKnownFlags(t *testing.T) {
	known := readKnownFlags(t, deployFlagList)

	yes := true
	number := 1
	widest := deployParams{
		Package:               "/srv/staging/pkg.tar.zst",
		Components:            "local-path-chart",
		Namespace:             "storage",
		Shasum:                "0000000000000000000000000000000000000000000000000000000000000000",
		Connected:             &yes,
		ForceConflicts:        &yes,
		SkipValuesSchemaValid: &yes,
		InsecureSkipTLSVerify: &yes,
		PlainHTTP:             &yes,
		TakeOwnership:         &yes,
		Retries:               &number,
		OCIConcurrency:        &number,
		Architecture:          "amd64",
		Cache:                 "/var/cache/zarf",
		Tmpdir:                "/var/tmp/zarf",
		Timeout:               "15m",
		PublicKey:             "/etc/zarf/packages.pub",
		Verify:                "always",
		LogLevel:              "debug",
		LogFormat:             "json",
		Values:                []string{"/etc/zarf/values.yaml"},
		SetValues:             map[string]string{"key": "value"},
		SetVariables:          map[string]string{"KEY": "value"},
	}

	for _, arg := range buildDeployArgs(&widest) {
		name, ok := strings.CutPrefix(arg, "--")
		if !ok {
			continue
		}
		name, _, _ = strings.Cut(name, "=")
		if !known[name] {
			t.Errorf("the module renders --%s, which %s does not list", name, deployFlagList)
		}
	}
}

func TestRunPackageDeployRequiresAPackage(t *testing.T) {
	args := argsFor(t, map[string]any{
		"kubeconfig": "/etc/rancher/rke2/rke2.yaml",
	})
	resp := &Response{Zarf: &Detail{Module: "package_deploy"}}

	err := runPackageDeploy(context.Background(), &fakeRunner{}, args, resp)
	if err == nil {
		t.Fatal("runPackageDeploy accepted parameters naming no package")
	}
	if !strings.Contains(err.Error(), "package") {
		t.Errorf("the error did not name the missing parameter: %v", err)
	}
}

func TestRunPackageDeployReportsTheRun(t *testing.T) {
	dir := t.TempDir()

	args := argsFor(t, map[string]any{
		"package":     "oci://ghcr.io/colonel-byte/zarf/csi-local-path-provider:0.0.37-upstream",
		"zarf_binary": "/usr/bin/zarf",
		"kubeconfig":  "/etc/rancher/rke2/rke2.yaml",
		"components":  "local-path-images,local-path-chart",
		"status_file": filepath.Join(dir, "status.json"),
	})
	resp := &Response{Zarf: &Detail{Module: "package_deploy"}}
	run := &fakeRunner{
		lines: []string{
			`{"msg":"Loading Zarf Package"}`,
			`{"msg":"deploying component","name":"local-path-images"}`,
			`{"msg":"deploying component","name":"local-path-chart"}`,
		},
	}

	if err := runPackageDeploy(context.Background(), run, args, resp); err != nil {
		t.Fatalf("runPackageDeploy reported %v", err)
	}

	if got, want := strings.Join(resp.Zarf.ComponentsRan, ","), "local-path-images,local-path-chart"; got != want {
		t.Errorf("componentsRan was %q, want %q", got, want)
	}
	if !resp.Changed || resp.Zarf.ChangedSignal != SignalPartial {
		t.Errorf("the run reported changed=%v signal=%q", resp.Changed, resp.Zarf.ChangedSignal)
	}
	if !strings.Contains(resp.Msg, "csi-local-path-provider") {
		t.Errorf("the message does not name what was deployed: %q", resp.Msg)
	}
	if run.got.Dir != "" {
		t.Errorf("zarf ran in %q, want the directory Ansible chose", run.got.Dir)
	}
}

func TestRunPackageDeployCheckModeSkips(t *testing.T) {
	args := argsFor(t, map[string]any{
		"package":             "oci://ghcr.io/colonel-byte/zarf/csi-local-path-provider:0.0.37-upstream",
		"_ansible_check_mode": true,
	})
	resp := &Response{Zarf: &Detail{Module: "package_deploy"}}
	run := &fakeRunner{}

	if err := runPackageDeploy(context.Background(), run, args, resp); err != nil {
		t.Fatalf("runPackageDeploy reported %v", err)
	}
	if !resp.Skipped || resp.Changed {
		t.Errorf("a check-mode run reported skipped=%v changed=%v", resp.Skipped, resp.Changed)
	}
	if run.got.Binary != "" {
		t.Errorf("a check-mode run executed %q", run.got.Binary)
	}
}

func TestModulesAnswersBothActions(t *testing.T) {
	got := strings.Join(Modules(), ",")
	if want := "init,package_deploy"; got != want {
		t.Errorf("Modules() = %q, want %q", got, want)
	}
}
