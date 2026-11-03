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

package phase

import (
	"testing"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/stretchr/testify/require"
)

// policyPhase is a phase wired to the distro config under test, with the host filter already
// resolved, so ShouldRun can be exercised without a connection.
func policyPhase(hosts cluster.Hosts, cfg distro.SELinux) *PrepareSelinuxPolicy {
	d := &distro.Distro{}
	d.Spec.Config.OS.SELinux = cfg

	p := &PrepareSelinuxPolicy{selinuxPolicyHosts: hosts}
	p.SetManager(&Manager{Distro: d})

	return p
}

// TestPrepareSelinuxPolicyShouldRun pins the two-part gate. Either half alone has to keep the
// phase off: a host that cannot take a policy must not be touched, and a package that asks for
// no policy must not make the phase run against every enforcing host in the fleet.
func TestPrepareSelinuxPolicyShouldRun(t *testing.T) {
	hosts := cluster.Hosts{&cluster.Host{Hostname: "control1"}}

	tests := []struct {
		name  string
		hosts cluster.Hosts
		cfg   distro.SELinux
		want  bool
	}{
		{
			name:  "no capable hosts",
			hosts: nil,
			cfg: distro.SELinux{
				Booleans: map[string]bool{"container_manage_cgroup": true},
			},
			want: false,
		},
		{
			name:  "no policy configured",
			hosts: hosts,
			cfg:   distro.SELinux{},
			want:  false,
		},
		{
			name:  "booleans only",
			hosts: hosts,
			cfg: distro.SELinux{
				Booleans: map[string]bool{"container_manage_cgroup": true},
			},
			want: true,
		},
		{
			name:  "modules only",
			hosts: hosts,
			cfg: distro.SELinux{
				Modules: []distro.SELinuxModule{
					{
						Name: "cargoship-engine",
						CIL:  "(allow kernel_t self (capability (sys_admin)))",
					},
				},
			},
			want: true,
		},
		{
			name:  "file contexts only",
			hosts: hosts,
			cfg: distro.SELinux{
				FileContexts: []distro.SELinuxFileContext{
					{
						Path: "/var/lib/rancher(/.*)?",
						Type: "container_var_lib_t",
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, policyPhase(tt.hosts, tt.cfg).ShouldRun())
		})
	}
}

// TestSelinuxLiteralPrefix covers the regular-expression-to-path step. restorecon takes a real
// path, so a prefix that still carries a metacharacter would relabel nothing, and a prefix that
// collapses to "/" would relabel the entire filesystem.
func TestSelinuxLiteralPrefix(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "trailing group",
			path: "/var/lib/rancher(/.*)?",
			want: "/var/lib/rancher",
		},
		{
			name: "trailing wildcard",
			path: "/opt/cni/bin/.*",
			want: "/opt/cni/bin",
		},
		{
			name: "metacharacter mid segment",
			path: "/var/lib/ran*her",
			want: "/var/lib/ran",
		},
		{
			name: "character class",
			path: "/etc/kubernetes/[a-z]*",
			want: "/etc/kubernetes",
		},
		{
			name: "fully literal path keeps its dots",
			path: "/etc/kubernetes/admin.conf",
			want: "/etc/kubernetes/admin.conf",
		},
		{
			name: "trailing separator removed",
			path: "/var/lib/kubelet/",
			want: "/var/lib/kubelet",
		},
		{
			name: "root only is refused",
			path: "/(.*)?",
			want: "",
		},
		{
			name: "relative path is refused",
			path: "var/lib/kubelet",
			want: "",
		},
		{
			name: "empty path is refused",
			path: "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, selinuxLiteralPrefix(tt.path))
		})
	}
}

// TestSelinuxBooleanArgsIsSorted pins the deterministic command. The map iteration order would
// otherwise change the setsebool call between runs, which makes a dry-run diff unreadable.
func TestSelinuxBooleanArgsIsSorted(t *testing.T) {
	booleans := map[string]bool{
		"virt_use_nfs":               false,
		"container_manage_cgroup":    true,
		"domain_kernel_load_modules": true,
	}

	names := []string{"container_manage_cgroup", "domain_kernel_load_modules", "virt_use_nfs"}

	require.Equal(
		t,
		[]string{"container_manage_cgroup=on", "domain_kernel_load_modules=on", "virt_use_nfs=off"},
		selinuxBooleanArgs(booleans, names),
	)
}

// TestSelinuxFcontextCommand checks the three operations share one renderer, so an added mapping
// and the removal that undoes it cannot drift apart.
func TestSelinuxFcontextCommand(t *testing.T) {
	fc := distro.SELinuxFileContext{
		Path: "/var/lib/rancher(/.*)?",
		Type: "container_var_lib_t",
	}

	tests := []struct {
		name string
		op   string
		want string
	}{
		{
			name: "add",
			op:   "-a",
			want: `semanage fcontext -a -t 'container_var_lib_t' -f 'a' '/var/lib/rancher(/.*)?'`,
		},
		{
			name: "modify",
			op:   "-m",
			want: `semanage fcontext -m -t 'container_var_lib_t' -f 'a' '/var/lib/rancher(/.*)?'`,
		},
		{
			name: "delete",
			op:   "-d",
			want: `semanage fcontext -d -t 'container_var_lib_t' -f 'a' '/var/lib/rancher(/.*)?'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, selinuxFcontextCommand(tt.op, fc))
		})
	}
}

// TestSelinuxFcontextCommandUsesTheConfiguredFileType makes sure an explicit file type reaches
// semanage, since a mapping restricted to directories behaves differently from one that is not.
// It reaches semanage as the letter semanage takes, not as the word the package declared: see
// TestSelinuxFileTypeTranslatesTheSchemaWords.
func TestSelinuxFcontextCommandUsesTheConfiguredFileType(t *testing.T) {
	fc := distro.SELinuxFileContext{
		Path:     "/var/lib/kubelet",
		Type:     "container_var_lib_t",
		FileType: "dir",
	}

	require.Equal(
		t,
		`semanage fcontext -a -t 'container_var_lib_t' -f 'd' '/var/lib/kubelet'`,
		selinuxFcontextCommand("-a", fc),
	)
}

// TestSelinuxModulePriority covers the default. 400 sits above the distribution policy's 100, so
// a package module overrides the shipped policy rather than conflicting with it.
func TestSelinuxModulePriority(t *testing.T) {
	tests := []struct {
		name   string
		module distro.SELinuxModule
		want   int
	}{
		{
			name:   "unset defaults above the distribution policy",
			module: distro.SELinuxModule{Name: "cargoship-engine"},
			want:   DefaultSELinuxModulePriority,
		},
		{
			name: "explicit priority is kept",
			module: distro.SELinuxModule{
				Name:     "cargoship-engine",
				Priority: 500,
			},
			want: 500,
		},
		{
			name: "a negative priority falls back to the default",
			module: distro.SELinuxModule{
				Name:     "cargoship-engine",
				Priority: -1,
			},
			want: DefaultSELinuxModulePriority,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, selinuxModulePriority(tt.module))
		})
	}
}

// TestSelinuxModulePath pins where the CIL source lands, because the removal phase rebuilds the
// same path from the module name recorded in the state file.
func TestSelinuxModulePath(t *testing.T) {
	require.Equal(t, "/var/lib/cargoship/selinux/cargoship-engine.cil", selinuxModulePath("cargoship-engine"))
}

// TestShellQuote covers the quoting, since a path out of the distro package reaches a shell.
func TestShellQuote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain path",
			input: "/var/lib/kubelet",
			want:  `'/var/lib/kubelet'`,
		},
		{
			name:  "path with a space",
			input: "/var/lib/my data",
			want:  `'/var/lib/my data'`,
		},
		{
			name:  "embedded single quote is escaped",
			input: "/var/lib/it's",
			want:  `'/var/lib/it'\''s'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shellQuote(tt.input))
		})
	}
}

// TestSelinuxFileTypeTranslatesTheSchemaWords is a defect that made the whole SELinux policy
// phase fail on any package carrying file contexts.
//
// The package schema documents the file type as a word -- all, file, dir -- and `semanage
// fcontext -f` takes the letter ls shows in its mode field, rejecting everything else with
// "argument -f/--ftype: invalid choice". The default was "all", so a package that declared a
// mapping and set no file type could not apply one at all: the upstream examples failed on every
// host, while k3s and rke2 never noticed because they stage a *-selinux RPM instead.
func TestSelinuxFileTypeTranslatesTheSchemaWords(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		fileType string
		want     string
	}{
		{
			name:     "unset is every kind of file",
			fileType: "",
			want:     "a",
		},
		{
			name:     "all",
			fileType: "all",
			want:     "a",
		},
		{
			name:     "file",
			fileType: "file",
			want:     "f",
		},
		{
			name:     "dir",
			fileType: "dir",
			want:     "d",
		},
		{
			name:     "symlink is l, which is the one no abbreviation would have guessed",
			fileType: "symlink",
			want:     "l",
		},
		{
			name:     "socket",
			fileType: "socket",
			want:     "s",
		},
		{
			name:     "pipe",
			fileType: "pipe",
			want:     "p",
		},
		{
			name:     "block",
			fileType: "block",
			want:     "b",
		},
		{
			name:     "char",
			fileType: "char",
			want:     "c",
		},
		{
			name:     "a letter semanage already takes is passed through",
			fileType: "d",
			want:     "d",
		},
		{
			name:     "and so is anything neither vocabulary knows, so semanage names it",
			fileType: "directory",
			want:     "directory",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := selinuxFileType(distro.SELinuxFileContext{
				Path:     "/var/lib/kubelet(/.*)?",
				Type:     "container_var_lib_t",
				FileType: tc.fileType,
			})
			require.Equal(t, tc.want, got)
		})
	}
}

// TestSelinuxFcontextCommandUsesTheTranslatedType holds the rendered command, which is what the
// host actually rejected.
func TestSelinuxFcontextCommandUsesTheTranslatedType(t *testing.T) {
	t.Parallel()

	cmd := selinuxFcontextCommand("-a", distro.SELinuxFileContext{
		Path: "/var/lib/kubelet(/.*)?",
		Type: "container_var_lib_t",
	})
	require.Contains(t, cmd, "-f 'a'")
	require.NotContains(t, cmd, "all")
}
