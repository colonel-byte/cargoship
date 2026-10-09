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

package tofu

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/distro"
	"github.com/colonel-byte/cargoship/test"
	"github.com/stretchr/testify/require"
)

// clusterName is the cluster the configuration describes, and the resource's identity.
const clusterName = "bootloose-tofu"

// TestTofuProvider walks one configuration through its whole life: initialised against the local
// mirror, planned, applied onto three machines, converged a second time, reduced by one worker
// through the `state = "absent"` marker, and destroyed.
//
// It is one test with ordered subtests rather than several top-level tests, because every step
// starts from what the step before it left on the hosts and in state. A step that fails stops the
// walk where continuing would assert against a cluster that was never built, and the destroy runs
// either way so a failed run does not leave an installed engine behind.
func TestTofuProvider(t *testing.T) {
	mirror := requireTofu(t)
	requireCluster(t)

	w := newWorkspace(t, mirror)
	pkgPath := buildPackage(t)
	hosts, loadBalancer := requireFleetVars(t)

	vars := map[string]any{
		"name":          clusterName,
		"load_balancer": loadBalancer,
		"package":       pkgPath,
		"hosts":         hosts,
		"timeout":       phaseTimeout(),
	}
	w.writeVars(t, vars)

	// The destroy is registered before the first apply, so a failure anywhere in the walk still
	// takes the engine off the machines. It is the only step that has to run, because the
	// machines outlive a single test when the run was asked to keep them.
	t.Cleanup(func() { destroyCluster(t, w) })

	if !t.Run("init", func(t *testing.T) { initWorkspace(t, w) }) {
		return
	}
	if !t.Run("plan", func(t *testing.T) { planCreatesTheCluster(t, w) }) {
		return
	}
	if !t.Run("apply", func(t *testing.T) { applyInstallsTheCluster(t, w) }) {
		return
	}
	t.Run("kubeconfig", func(t *testing.T) { exportedKubeconfigReachesTheCluster(t, w) })
	t.Run("converge", func(t *testing.T) { secondApplyChangesNothing(t, w) })
	t.Run("remove", func(t *testing.T) { absentHostLeavesTheCluster(t, w, vars) })
}

// initWorkspace is the step that proves the provider resolves without a registry: the module
// names `colonel-byte/cargoship` with a version constraint, and the only thing serving it is the
// filesystem mirror the workspace's CLI configuration points at.
//
// The version is read off what init printed, and where the provider came from is read off the
// filesystem. Init names the provider in its short form -- `colonel-byte/cargoship` -- so the
// output says nothing about the source address; the directory OpenTofu linked the provider into
// is addressed by the full one, which is what tells a mirror install from a registry install.
func initWorkspace(t *testing.T, w *workspace) {
	out := w.run(t, "init", "-input=false")
	require.Contains(t, out, "Installing colonel-byte/cargoship v"+providerVersion,
		"init did not install the version the mirror holds")

	installed := filepath.Join(w.dir, ".terraform", "providers", providerSource, providerVersion, hostPlatform())
	_, err := os.Lstat(installed)
	require.NoError(t, err, "init did not resolve the provider through the mirror: %s is not there", installed)
}

// planCreatesTheCluster reads the plan rather than only checking that it succeeded. A plan that
// errors is obvious; a plan that quietly plans nothing because the module took no hosts is not.
func planCreatesTheCluster(t *testing.T, w *workspace) {
	out := w.run(t, "plan", "-input=false")
	require.Contains(t, out, "cargoship_cluster", "the plan holds no cargoship_cluster resource")
	require.Contains(t, out, "1 to add", "the plan creates nothing")
}

// applyInstallsTheCluster is the apply, and then the assertions that it installed a cluster
// rather than reporting that it had.
//
// The nodes are counted from the engine's own view through the controller's kubectl, not from the
// provider's `nodes` attribute. The attribute is what the provider believes, and a provider that
// believed the wrong thing is exactly the failure worth catching here.
func applyInstallsTheCluster(t *testing.T, w *workspace) {
	w.run(t, "apply", "-input=false", "-auto-approve")

	state := w.clusterFromState(t)
	require.Equal(t, "k3s", state.Distro, "the engine in state is not the one the package carries")
	require.Equal(t, enginePackage.version, state.EngineVersion)
	require.Len(t, state.Nodes, nodeCount, "the resource reported a different number of nodes")
	require.Len(t, state.Hosts, nodeCount)
	for name, host := range state.Hosts {
		require.False(t, host.Removed, "host %s is marked removed after an install", name)
	}

	require.Equal(t, enginePackage.version, w.stringOutput(t, "engine_version"),
		"the module's engine_version output does not carry the package's version")

	requireNodeNames(t, nodeCount)
}

// exportedKubeconfigReachesTheCluster writes the credentials the apply returned to KUBECONFIG and
// uses them. `export_kubeconfig` is the attribute that decides whether they come back at all, and
// a value that is present but unusable looks the same in state as one that works.
func exportedKubeconfigReachesTheCluster(t *testing.T, w *workspace) {
	kubeconfig := w.stringOutput(t, "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600))

	cs, err := e2e.KubeClient(t)
	require.NoError(t, err)
	require.NoError(t, test.WaitForNodesReady(context.Background(), cs, nodeCount, 5*time.Minute),
		"the nodes did not all report Ready through the exported kubeconfig")
}

// secondApplyChangesNothing is the convergence claim: the phases route through their upgrade and
// no-op paths on a cluster that is already installed, so a second apply over an unchanged
// configuration reports no changes rather than re-bootstrapping.
//
// This is also where a computed attribute that cannot be promised shows up. An attribute whose
// plan says "known after apply" on every run would leave the resource perpetually in need of an
// update, and a plan that promised a value the apply then contradicted would fail outright.
func secondApplyChangesNothing(t *testing.T, w *workspace) {
	out := w.run(t, "plan", "-input=false")
	require.Contains(t, out, "No changes",
		"a second plan over an unchanged configuration still wants to change something")
}

// absentHostLeavesTheCluster is the removal, in one apply: the worker is marked absent, the apply
// drains and deletes it through a controller that stays and uninstalls its engine, and the marker
// that records the work is read back out of state.
//
// The last worker is the one removed, so a controller and a worker are both left -- the removal
// has to be driven through a surviving controller, and a cluster left with only controllers would
// not prove the hosts that stay were left alone.
func absentHostLeavesTheCluster(t *testing.T, w *workspace, vars map[string]any) {
	hosts, ok := vars["hosts"].(map[string]hostVars)
	require.True(t, ok, "the fleet in the variables is a %T", vars["hosts"])

	removed := fmt.Sprintf(bootKW, workers-1)
	host, ok := hosts[removed]
	require.True(t, ok, "the fleet holds no machine named %s", removed)
	host.State = "absent"
	hosts[removed] = host
	w.writeVars(t, vars)

	w.run(t, "apply", "-input=false", "-auto-approve")

	state := w.clusterFromState(t)
	require.Len(t, state.Hosts, nodeCount, "the removed host was pruned from state rather than marked")
	require.True(t, state.Hosts[removed].Removed,
		"%s is still not marked removed, so the next apply would tear it down again", removed)
	for name, host := range state.Hosts {
		if name == removed {
			continue
		}
		require.False(t, host.Removed, "host %s was marked removed by a run that did not target it", name)
	}

	// The node is gone from the cluster, and the engine is gone from the machine. The second
	// half is what separates a removal from a `kubectl delete node`.
	requireNodeNames(t, nodeCount-1)
	out, err := machineExec(removed, "systemctl", "is-active", "k3s-agent")
	require.Error(t, err, "the engine is still running on %s: %s", removed, out)
}

// destroyCluster tears the cluster down and checks that state was emptied.
//
// It runs from t.Cleanup, so it also runs after a failed step, and it reports rather than fails
// when the walk never got as far as an apply: a destroy over empty state is not a failure.
func destroyCluster(t *testing.T, w *workspace) {
	if w.stateIsEmpty(t) {
		t.Log("nothing in state to destroy")
		return
	}

	out, err := w.tryRun(t, "destroy", "-input=false", "-auto-approve")
	if err != nil {
		t.Errorf("destroy failed, which leaves an installed engine on the machines: %v\n%s", err, out)
		return
	}
	require.True(t, w.stateIsEmpty(t), "destroy finished with the resource still in state")

	// A destroy resets the cluster, so the controller's engine is no longer serving.
	controller := fmt.Sprintf(bootKC, 0)
	if out, err := machineExec(controller, "systemctl", "is-active", "k3s-server"); err == nil {
		t.Errorf("the engine is still running on %s after a destroy: %s", controller, out)
	}
}

// requireNodeNames waits until the controller's own kubectl reports want nodes, and fails with
// what it saw instead.
//
// The wait is here because a removal is not instantaneous from the API server's side: the node
// object goes when the controller deletes it, which the apply has already done by the time it
// returns, but a cluster under the load of three nested containers can take a moment to agree.
func requireNodeNames(t *testing.T, want int) {
	t.Helper()

	controller := fmt.Sprintf(bootKC, 0)
	deadline := time.Now().Add(2 * time.Minute)
	var last string
	for {
		out, err := machineExec(controller, "k3s", "kubectl", "get", "nodes", "--no-headers")
		if err == nil {
			last = strings.TrimSpace(out)
			if last != "" && len(strings.Split(last, "\n")) == want {
				return
			}
		} else {
			last = fmt.Sprintf("%v: %s", err, out)
		}
		if time.Now().After(deadline) {
			t.Fatalf("the cluster did not settle at %d nodes, it reports:\n%s", want, last)
		}
		time.Sleep(5 * time.Second)
	}
}

// buildPackage builds the distro package the configuration installs, from the shipped example
// definition with the sysctls a container cannot apply removed.
//
// It builds rather than reusing a checked-in artifact for the reason the cluster suite does: the
// definitions under example/ are what cargoship ships, so a package built from them is the one an
// operator would have, and a fixture would drift from them silently.
func buildPackage(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	definition, err := test.ContainerSafeDefinition(enginePackage.path, dir)
	require.NoError(t, err)

	if config.CommonOptions.CachePath == "" {
		config.CommonOptions.CachePath = config.DefaultCachePath
	}
	if config.CommonOptions.TempDirectory == "" {
		config.CommonOptions.TempDirectory = os.TempDir()
	}
	cache, err := config.GetAbsCachePath()
	require.NoError(t, err)

	pkgPath, err := distro.Create(context.Background(), definition, dir, distro.CreateOptions{
		Architecture: config.CLIArch,
		CachePath:    cache,
	})
	require.NoError(t, err)
	require.FileExists(t, pkgPath)
	return pkgPath
}

// requireFleetVars reads the live machines into the module's host map.
func requireFleetVars(t *testing.T) (map[string]hostVars, string) {
	t.Helper()

	keyPath, err := absKeyPath()
	require.NoError(t, err)

	hosts, loadBalancer, err := fleetVars(testCluster, keyPath)
	require.NoError(t, err)
	require.Len(t, hosts, nodeCount)
	return hosts, loadBalancer
}
