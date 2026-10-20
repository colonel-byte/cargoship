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
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// fapolicydPhase is the phase with its host filter already resolved, so the warning can be
// exercised without a host to ask whether fapolicyd is running on.
func fapolicydPhase(hosts cluster.Hosts, rules string) *PrepareFapolicy {
	d := &distro.Distro{}
	d.Spec.Config.OS.FAPolicyd = rules

	p := &PrepareFapolicy{fapolicydhosts: hosts}
	p.SetManager(&Manager{Distro: d})

	return p
}

// TestPrepareFapolicyWarnsWhenThePackageCarriesNoRules is the silence that cost an afternoon.
//
// ShouldRun is false when the package declares no rules, so the phase was skipped without a word
// on a fleet whose hosts all run fapolicyd. The cluster then installed and could not start a
// container: fapolicyd denied the runtime's executions out of its own content store, and what
// surfaced was `runc create failed: ... write init-p: broken pipe`, which names neither
// fapolicyd nor the package that should have carried rules for it.
func TestPrepareFapolicyWarnsWhenThePackageCarriesNoRules(t *testing.T) {
	hosts := cluster.Hosts{&cluster.Host{Hostname: "control1"}}

	for _, tc := range []struct {
		name     string
		hosts    cluster.Hosts
		rules    string
		wantWarn bool
	}{
		{
			name:     "hosts run fapolicyd and the package says nothing",
			hosts:    hosts,
			rules:    "",
			wantWarn: true,
		},
		{
			name:     "the package carries rules, so the phase writes them",
			hosts:    hosts,
			rules:    "allow perm=any all : dir=/var/lib/containerd/\n",
			wantWarn: false,
		},
		{
			name:     "no host runs fapolicyd, so there is nothing to warn about",
			hosts:    nil,
			rules:    "",
			wantWarn: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			ctx := logger.WithContext(context.Background(),
				slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))

			fapolicydPhase(tc.hosts, tc.rules).warnOnMissingRules(ctx)

			if tc.wantWarn {
				require.Contains(t, buf.String(), "declares no rules")
				require.Contains(t, buf.String(), FAPolicydRuleFile,
					"the warning does not name the file it would have written")
				return
			}
			require.Empty(t, buf.String())
		})
	}
}

// TestPrepareFapolicyWarnSurvivesAMissingDistro keeps the warning off the paths that have no
// package at all: a reset loads none, and dereferencing one here would panic rather than warn.
func TestPrepareFapolicyWarnSurvivesAMissingDistro(t *testing.T) {
	var buf bytes.Buffer
	ctx := logger.WithContext(context.Background(),
		slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))

	p := &PrepareFapolicy{fapolicydhosts: cluster.Hosts{&cluster.Host{Hostname: "control1"}}}
	p.SetManager(&Manager{})

	require.NotPanics(t, func() { p.warnOnMissingRules(ctx) })
	require.Empty(t, buf.String())
}

func TestPrepareFapolicyExecutableRules(t *testing.T) {
	d := &distro.Distro{}
	d.Spec.Config.Files = v1alpha1.Files{
		&v1alpha1.File{Target: "/usr/local/bin/etcdctl", Executable: true},
		&v1alpha1.File{Target: "/usr/local/bin/not-exec", Executable: false},
	}
	d.Spec.Config.OS.Files = v1alpha1.Files{
		&v1alpha1.File{Target: "/usr/local/bin/rke2", Executable: true},
		&v1alpha1.File{Target: "/usr/local/bin/rke2-killall.sh", Executable: true},
		&v1alpha1.File{Target: "/usr/local/bin/rke2", Executable: true}, // duplicate to test deduplication
	}

	p := &PrepareFapolicy{}
	p.SetManager(&Manager{Distro: d})

	rules := p.executableRules()
	require.Equal(t, "allow perm=any all : path=/usr/local/bin/etcdctl\nallow perm=any all : path=/usr/local/bin/rke2\nallow perm=any all : path=/usr/local/bin/rke2-killall.sh\n", rules)
}
