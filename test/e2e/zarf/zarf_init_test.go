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

package zarf

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// initComponents are the components the standard init package deploys. All four are required in
// that package -- only the slim init the uds-bundle uses makes them optional -- so every
// initialisation reports all four, and the suite names them rather than counting them.
var initComponents = []string{ //nolint:gochecknoglobals
	"zarf-injector",
	"zarf-seed-registry",
	"zarf-registry",
	"zarf-agent",
}

// storageComponents are the components of the uds-bundle's local-path provider package.
var storageComponents = []string{ //nolint:gochecknoglobals
	"local-path-images",
	"local-path-chart",
}

// TestBundleOrderWalk runs the uds-bundle's four packages in the bundle's own order, on a cluster
// that has k3s's StorageClass: init, storage provider, init, storage provider.
//
// This is the walk that proves the pair of modules alternates the way a bundle deploy does, and
// that a second init over an already-initialised cluster reconciles rather than fails.
func TestBundleOrderWalk(t *testing.T) {
	requireSuite(t)

	cluster := withCluster(t, storedConfig, storedCluster)

	// The contracts that need a cluster nothing has touched run first, on this cluster, rather
	// than paying for one of their own: check mode has to be able to assert that nothing ran.
	t.Run("module contracts", func(t *testing.T) { testModuleContracts(t, cluster) })

	out, results := cluster.walk(t, bundleOrderPlaybook)

	assertInitStep(t, readStep(t, results, "1-init.json"), out)
	assertDeployStep(t, readStep(t, results, "2-storage.json"), out)
	assertInitStep(t, readStep(t, results, "3-reinit.json"), out)
	assertDeployStep(t, readStep(t, results, "4-restorage.json"), out)

	assertClusterInitialised(t, cluster)
	assertStorageClass(t, cluster)
	assertRegistryPVCBound(t, cluster)
	assertRegistryProxyMode(t, cluster)
}

// TestProviderFirstWalk runs the same four packages on the uds-bundles k3d config as it is
// written, where k3s local-storage is disabled and the cluster therefore has no StorageClass.
//
// The order has to change -- the provider is deployed first, connected, so that the registry has
// somewhere to claim its volume from -- and that reordering is the thing being proved. A
// connected deploy needs no registry, so it is the one zarf operation that works on a cluster
// nothing has initialised.
func TestProviderFirstWalk(t *testing.T) {
	requireSuite(t)

	cluster := withCluster(t, bareConfig, bareCluster)

	// The premise: nothing has initialised this cluster and it has no StorageClass at all.
	assertNoStorageClass(t, cluster)

	out, results := cluster.walk(t, providerFirstPlaybook)

	assertDeployStep(t, readStep(t, results, "1-storage.json"), out)
	assertInitStep(t, readStep(t, results, "2-init.json"), out)
	assertDeployStep(t, readStep(t, results, "3-restorage.json"), out)
	assertInitStep(t, readStep(t, results, "4-reinit.json"), out)

	assertClusterInitialised(t, cluster)
	assertStorageClass(t, cluster)
	assertRegistryPVCBound(t, cluster)
	assertRegistryProxyMode(t, cluster)
}

// testModuleContracts holds the contracts a playbook author depends on and that need no walk: a
// check-mode task is skipped without touching the cluster, and a failure is a module result rather
// than a crash.
//
// It drives the wrappers directly, which is the path a third-party caller of the module symlinks
// takes, and the one the walks do not cover.
func testModuleContracts(t *testing.T, cluster *cluster) {
	t.Run("init check mode changes nothing", func(t *testing.T) {
		result, _ := runWrapper(t, suite.initWrapper, "init", map[string]any{
			"init_package":        suite.initPackage,
			"kubeconfig":          cluster.kubeconfig,
			"_ansible_check_mode": true,
		})
		assertSkipped(t, result)

		clientset := kubeClient(t, cluster.kubeconfig)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := clientset.CoreV1().Namespaces().Get(ctx, "zarf", metav1.GetOptions{}); err == nil {
			t.Error("check mode created the zarf namespace, so something ran")
		}
	})

	t.Run("a custom init package is deployed by path", func(t *testing.T) {
		// Zarf's own lookup finds a package named after zarf's version in a directory it chooses.
		// A copy under a name that lookup cannot find is what proves the module names the package
		// rather than relying on it: the only way this run reaches a package at all is the
		// positional source zarf init has taken since v0.72.0.
		custom := filepath.Join(t.TempDir(), "our-init-package.tar.zst")
		staged, err := os.ReadFile(suite.initPackage)
		if err != nil {
			t.Fatalf("unable to read the staged init package: %v", err)
		}
		if err := os.WriteFile(custom, staged, 0o600); err != nil {
			t.Fatalf("unable to stage the renamed init package: %v", err)
		}

		// The cluster is already initialised by the walk that ran before this, so the run is a
		// re-init: it reaches zarf, loads the package, and leaves the cluster as it found it.
		result, out := runWrapper(t, suite.initWrapper, "init", map[string]any{
			"init_package": custom,
			"kubeconfig":   cluster.kubeconfig,
			"components":   "zarf-agent",
			"timeout":      "10m",
		})
		if boolField(result, "failed") {
			t.Fatalf("the module failed against a renamed init package: %v; output %s", result, out)
		}

		detail := zarfDetail(t, result)
		command := stringsOf(t, detail["command"])
		if len(command) < 3 || command[1] != "init" || command[2] != custom {
			t.Errorf("the command line was %v, want the renamed package as zarf init's positional source", command)
		}
		if version := stringField(detail, "version"); version == "" {
			t.Error("the result carried no zarf version, so the floor check did not run")
		}
	})

	t.Run("deploy check mode changes nothing", func(t *testing.T) {
		result, _ := runWrapper(t, suite.deployWrapper, "package_deploy", map[string]any{
			"package":             suite.storagePackage,
			"kubeconfig":          cluster.kubeconfig,
			"_ansible_check_mode": true,
		})
		assertSkipped(t, result)

		clientset := kubeClient(t, cluster.kubeconfig)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		classes, err := clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
		if err != nil {
			t.Fatalf("unable to list storage classes: %v", err)
		}
		for _, class := range classes.Items {
			if class.Provisioner == "rancher.io/local-path" && strings.HasPrefix(class.Name, "local-path") {
				continue // k3s's own, which this cluster ships with
			}
			t.Errorf("check mode left a storage class behind: %s", class.Name)
		}
	})

	t.Run("a failed deploy is a module result rather than a crash", func(t *testing.T) {
		// A package that is not there. Pointing at a missing file is the mistake an operator
		// actually makes -- a staging path that was never populated -- and it is the cheapest way
		// to reach zarf's own failure path.
		missing := t.TempDir() + "/zarf-package-absent-amd64.tar.zst"

		result, stderr := runWrapper(t, suite.deployWrapper, "package_deploy", map[string]any{
			"package":    missing,
			"kubeconfig": cluster.kubeconfig,
			"timeout":    "2m",
		})

		if !boolField(result, "failed") {
			t.Fatalf("deploying a package that is not there reported success: %v", result)
		}
		if msg := stringField(result, "msg"); !strings.Contains(msg, "zarf package deploy failed") {
			t.Errorf("the failure message does not say what failed: %q", msg)
		}

		detail := zarfDetail(t, result)
		if numberField(detail, "exitCode") == 0 {
			t.Errorf("the failed result reported exit code 0: %v", detail)
		}
		// zarf's own diagnosis is what an operator needs, and it has to be on stderr: stdout
		// carries the one JSON object and nothing else.
		if strings.TrimSpace(stderr) == "" {
			t.Error("the module reported a failure and captured none of zarf's output on stderr")
		}
	})
}

// assertInitStep holds what an initialisation step reported: that it ran, that it named every
// component of the init package, and that the operator saw each one go by while it was running.
func assertInitStep(t *testing.T, result map[string]any, output string) {
	t.Helper()
	assertStepRan(t, result, initComponents, output)

	command := stringsOf(t, zarfDetail(t, result)["command"])
	// The three flags the module adds on its own behalf rather than from a parameter, plus the
	// one the playbook asks for. Registry proxy mode matters enough to assert: nodeport mode
	// pushes through a node port, and that push times out and retries on k3d.
	for _, want := range []string{"init", "--confirm", "--no-color", "proxy"} {
		if !contains(command, want) {
			t.Errorf("the reported command line is missing %s: %v", want, command)
		}
	}
}

// assertDeployStep holds what a storage provider deploy reported, including that it verified the
// package's signature against the key the playbook named: an unverified deploy would still
// succeed, so the flag has to be in the command line the module reports.
func assertDeployStep(t *testing.T, result map[string]any, output string) {
	t.Helper()
	assertStepRan(t, result, storageComponents, output)

	command := stringsOf(t, zarfDetail(t, result)["command"])
	// --verify renders as one argument: pflag reads a flag with a no-argument default the way it
	// reads a boolean, so the space form would leave "always" as a positional argument.
	for _, want := range []string{"package", "deploy", "--confirm", "--verify=always", "--key"} {
		if !contains(command, want) {
			t.Errorf("the reported command line is missing %s: %v", want, command)
		}
	}
}

// assertStepRan is what every successful step has in common.
func assertStepRan(t *testing.T, result map[string]any, want []string, output string) {
	t.Helper()

	if boolField(result, "failed") {
		t.Fatalf("the step reported a failure: %v", result["msg"])
	}
	if !boolField(result, "changed") {
		t.Errorf("a step that ran reported changed: false: %v", result)
	}

	detail := zarfDetail(t, result)
	// Partial is the honest answer and the only one a wrapper can give: zarf reports which
	// components it deployed and not whether any of them found the cluster already as it wanted.
	// See docs/agent/choice-zarf-ansible-module.md.
	if signal := stringField(detail, "changedSignal"); signal != "partial" {
		t.Errorf("changedSignal was %q, want %q", signal, "partial")
	}

	ran := stringsOf(t, detail["componentsRan"])
	for _, component := range want {
		if !contains(ran, component) {
			t.Errorf("componentsRan does not name %s: %v", component, ran)
		}
	}

	// The heartbeat reached the operator's console. Without it a fifteen-minute task prints
	// nothing until it is over, which is the whole reason the side channel exists.
	for _, component := range ran {
		line := fmt.Sprintf("%s [running]", component)
		if !strings.Contains(output, line) {
			t.Errorf("the playbook output carries no progress line for %s (%q)", component, line)
		}
	}
}

func assertSkipped(t *testing.T, result map[string]any) {
	t.Helper()
	if !boolField(result, "skipped") {
		t.Errorf("check mode did not report the task as skipped: %v", result)
	}
	if boolField(result, "changed") {
		t.Errorf("check mode reported changed: %v", result)
	}
	if boolField(result, "failed") {
		t.Errorf("check mode reported a failure: %v", result)
	}
}

// assertClusterInitialised checks the cluster itself rather than trusting a module's report of
// what it did. These are the objects `zarf init` exists to create.
func assertClusterInitialised(t *testing.T, c *cluster) {
	t.Helper()
	clientset := kubeClient(t, c.kubeconfig)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if _, err := clientset.CoreV1().Namespaces().Get(ctx, "zarf", metav1.GetOptions{}); err != nil {
		t.Fatalf("the zarf namespace is not there: %v", err)
	}
	// zarf-state is how zarf itself knows the cluster is initialised; a later package deploy reads
	// it to find the registry.
	if _, err := clientset.CoreV1().Secrets("zarf").Get(ctx, "zarf-state", metav1.GetOptions{}); err != nil {
		t.Errorf("the zarf-state secret is not there: %v", err)
	}

	// A Deployment that exists but has no available replica is a component that reported success
	// and left nothing running, which is the failure these assertions are for.
	for _, name := range []string{"zarf-docker-registry", "agent-hook"} {
		deployment, err := clientset.AppsV1().Deployments("zarf").Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Errorf("the %s deployment is not there: %v", name, err)
			continue
		}
		if deployment.Status.AvailableReplicas == 0 {
			t.Errorf("the %s deployment has no available replicas", name)
		}
	}
}

// assertStorageClass checks that the provider package left the StorageClass behind. It is the
// whole reason the uds-bundle carries that package.
func assertStorageClass(t *testing.T, c *cluster) {
	t.Helper()
	clientset := kubeClient(t, c.kubeconfig)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	class, err := clientset.StorageV1().StorageClasses().Get(ctx, "local-path", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("the local-path storage class is not there: %v", err)
	}
	if class.Provisioner != "rancher.io/local-path" {
		t.Errorf("the local-path storage class is provisioned by %q", class.Provisioner)
	}
}

// assertNoStorageClass is the premise of the provider-first walk: the cluster the verbatim k3d
// config describes has no storage at all, which is why nothing zarf does can come first.
func assertNoStorageClass(t *testing.T, c *cluster) {
	t.Helper()
	clientset := kubeClient(t, c.kubeconfig)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	classes, err := clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("unable to list storage classes: %v", err)
	}
	if len(classes.Items) != 0 {
		names := make([]string, 0, len(classes.Items))
		for _, class := range classes.Items {
			names = append(names, class.Name)
		}
		t.Fatalf("the cluster already has storage classes %v, so the walk proves nothing; "+
			"%s is meant to disable k3s local-storage", names, bareConfig)
	}
}

// assertRegistryPVCBound checks that the registry's claim was satisfied by the provider, which is
// what joins the two halves of the walk together.
func assertRegistryPVCBound(t *testing.T, c *cluster) {
	t.Helper()
	clientset := kubeClient(t, c.kubeconfig)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	claim, err := clientset.CoreV1().PersistentVolumeClaims("zarf").
		Get(ctx, "zarf-docker-registry", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("the registry's claim is not there: %v", err)
	}
	if claim.Status.Phase != "Bound" {
		t.Errorf("the registry's claim is %s rather than Bound", claim.Status.Phase)
	}
	if class := claim.Spec.StorageClassName; class == nil || *class != "local-path" {
		t.Errorf("the registry's claim names storage class %v, want local-path", class)
	}
}

// assertRegistryProxyMode checks that the mode the playbook asked for is the mode zarf recorded.
// It is the one init parameter whose effect is visible in the cluster rather than only in the
// command line, so it is worth reading back.
func assertRegistryProxyMode(t *testing.T, c *cluster) {
	t.Helper()
	clientset := kubeClient(t, c.kubeconfig)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if _, err := clientset.AppsV1().DaemonSets("zarf").
		Get(ctx, "zarf-registry-proxy", metav1.GetOptions{}); err != nil {
		t.Errorf("the registry proxy daemonset is not there, so proxy mode did not take: %v", err)
	}

	secret, err := clientset.CoreV1().Secrets("zarf").Get(ctx, "zarf-state", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("the zarf-state secret is not there: %v", err)
	}
	var state struct {
		RegistryInfo struct {
			RegistryMode string `json:"registryMode"`
		} `json:"registryInfo"`
	}
	if err := json.Unmarshal(secret.Data["state"], &state); err != nil {
		t.Fatalf("unable to read zarf's state: %v", err)
	}
	if state.RegistryInfo.RegistryMode != "proxy" {
		t.Errorf("zarf recorded registry mode %q, want proxy", state.RegistryInfo.RegistryMode)
	}
}

func zarfDetail(t *testing.T, result map[string]any) map[string]any {
	t.Helper()
	detail, ok := result["zarf"].(map[string]any)
	if !ok {
		t.Fatalf("the result carried no zarf detail: %v", result)
	}
	return detail
}

// stringsOf reads a JSON array of strings out of a decoded result, failing the test when it is
// anything else. A missing key is an empty list, so the caller's assertion names what it wanted.
func stringsOf(t *testing.T, value any) []string {
	t.Helper()
	if value == nil {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("expected a list, got %T (%v)", value, value)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("expected a list of strings, got a %T in %v", item, value)
		}
		out = append(out, text)
	}
	return out
}

// boolField, stringField and numberField read one field out of a decoded module result. A field
// that is absent or of another type reads as the zero value, which is what every assertion here
// wants: the test says what it expected rather than failing on the shape.
func boolField(result map[string]any, key string) bool {
	value, ok := result[key].(bool)
	return ok && value
}

func stringField(result map[string]any, key string) string {
	value, ok := result[key].(string)
	if !ok {
		return ""
	}
	return value
}

func numberField(result map[string]any, key string) float64 {
	value, ok := result[key].(float64)
	if !ok {
		return 0
	}
	return value
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}
