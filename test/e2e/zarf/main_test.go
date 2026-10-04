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

// Package zarf holds the e2e suite for the colonel_byte.zarf Ansible collection: real k3d
// clusters, a real zarf, and the uds-bundle's four packages walked by the two modules.
//
// The suite proves the whole path rather than the Go half of it. The unit tests in
// internal/zarfmod cover the argument vectors, the WANT_JSON intake, and the progress reader
// against a fake runner; the container smoke checks in containers/ansible/Dockerfile cover the
// module contract with no cluster. What is left -- that the action plugins deliver parameters to
// the wrappers, that the command lines are ones zarf accepts, and that the components the modules
// report are the ones that reached the cluster -- needs a cluster, and that is what this is.
//
// Two clusters, because the walk differs by what storage the cluster has. See
// testdata/k3d-config.yaml and testdata/k3d-config-local-storage.yaml, and the playbooks beside
// them.
//
// It needs k3d, a zarf on PATH, ansible-playbook, Docker, and the network to pull three packages
// once. Nothing here runs under `go test ./...`: it is driven by `mage test:endToEndZarf`, and
// every test skips with a reason when a prerequisite is missing.
package zarf

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/test"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// The cluster fixtures and the names inside them. The names are spelled out here as well because
// the suite asks k3d about a cluster before it has parsed anything, and because the refusal to
// adopt an existing cluster must not depend on reading the file correctly.
const (
	bareConfig    = "test/e2e/zarf/testdata/k3d-config.yaml"
	bareCluster   = "zarf"
	storedConfig  = "test/e2e/zarf/testdata/k3d-config-local-storage.yaml"
	storedCluster = "zarf-local-storage"
)

const (
	providerFirstPlaybook = "test/e2e/zarf/testdata/provider-first.yml"
	bundleOrderPlaybook   = "test/e2e/zarf/testdata/bundle-order.yml"
	packagePublicKey      = "test/e2e/zarf/testdata/colonel-byte-zarf-packages.pub"
)

// The module files the suite drives. mage test:endToEndZarf builds them; see
// magefiles/pkg/build.ZarfModules.
const (
	initWrapper   = "build/zarf_init"
	deployWrapper = "build/zarf_package_deploy"
)

const (
	// packageCache is where the packages are kept between runs. It is under build/ because three
	// package downloads are not something to repeat on every run, and build/ is the directory a
	// clean removes.
	packageCache = "build/tmp/zarf-e2e"

	// storagePackage is the uds-bundle's local-path storage provider. The tag is pinned here
	// rather than taken from the bundle at run time: a suite that resolved it would be testing
	// whatever was published this morning.
	storagePackage = "oci://ghcr.io/colonel-byte/zarf/csi-local-path-provider:0.0.37-upstream"
	// storagePackageFile is what `zarf package pull` writes that reference to.
	storagePackageFile = "zarf-package-csi-local-path-provider-%s-0.0.37-upstream.tar.zst"

	// localPathVolume is the host directory both k3d configs mount into the nodes. k3d does not
	// create it, and a missing bind source fails the cluster create.
	localPathVolume = "/tmp/k3d"
)

// Environment variables the suite reads.
const (
	// envInitPackage names an init package to use instead of pulling one. It is what an offline
	// run sets, and what to set when testing against a package built from a branch.
	envInitPackage = "CARGOSHIP_E2E_ZARF_INIT_PACKAGE"
	// envStoragePackage names a storage provider package to use instead of pulling one.
	envStoragePackage = "CARGOSHIP_E2E_ZARF_STORAGE_PACKAGE"
	// envKeep leaves the clusters running after the suite, for looking at what the modules did.
	envKeep = "CARGOSHIP_E2E_ZARF_KEEP"
)

// environment is what the suite resolved at startup.
type environment struct {
	root           string
	arch           string
	zarf           string
	zarfVersion    string
	ansible        string
	initWrapper    string
	deployWrapper  string
	initPackage    string
	storagePackage string
	publicKey      string
	// collections is the directory handed to Ansible as its collections path. It holds an
	// ansible_collections symlink into the repository, which is the layout Ansible insists on and
	// the repository does not have.
	collections string
	// skip is why the suite cannot run, empty when it can.
	skip string
}

var suite environment //nolint:gochecknoglobals

func TestMain(m *testing.M) {
	e2e, root, err := test.BootstrapInProcess()
	if err != nil {
		log.Fatal(err)
	}
	suite.root = root
	suite.arch = e2e.Arch
	// Everything the suite spawns writes its temporaries somewhere, and under the mage runner that
	// somewhere is build/tmp, inside the repository: k3d leaves a copy of the config it was given
	// and a hosts file per cluster, zarf leaves a staging directory per deploy, and this suite
	// leaves the collections path it stages. Narrowing TMPDIR to one directory and removing it
	// afterwards is what keeps a run from accumulating all of that where a developer has to notice
	// it. Every child inherits it through os.Environ().
	runTmp, err := os.MkdirTemp("", "zarf-e2e-run-")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.Setenv("TMPDIR", runTmp); err != nil {
		log.Fatal(err)
	}

	suite.skip = prepare(&suite)

	code := m.Run()

	if err := os.RemoveAll(runTmp); err != nil {
		log.Printf("unable to remove the suite's temporary directory: %v", err)
	}

	os.Exit(code)
}

// prepare resolves everything the suite needs and returns why it cannot run, empty when it can.
//
// Nothing here is fatal. A developer without k3d installed should get a skip that says so, not a
// failure that reads as the modules being broken.
func prepare(env *environment) string {
	if runtime.GOOS != "linux" {
		return "the suite runs k3d, which needs Linux containers"
	}

	for _, tool := range []string{"docker", "k3d"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Sprintf("%s is not on PATH", tool)
		}
	}

	ansible, err := exec.LookPath("ansible-playbook")
	if err != nil {
		return "ansible-playbook is not on PATH: the suite drives the modules through a playbook, " +
			"because the action plugins are half of what it is proving"
	}
	env.ansible = ansible

	zarfPath, err := exec.LookPath("zarf")
	if err != nil {
		return "zarf is not on PATH: the modules run the installed zarf rather than embedding one"
	}
	env.zarf = zarfPath

	version, err := zarfReleaseVersion(zarfPath)
	if err != nil {
		return fmt.Sprintf("unable to read the zarf version: %v", err)
	}
	env.zarfVersion = version

	for _, wrapper := range []struct {
		path string
		into *string
	}{
		{path: initWrapper, into: &env.initWrapper},
		{path: deployWrapper, into: &env.deployWrapper},
	} {
		absolute := filepath.Join(env.root, wrapper.path)
		if _, err := os.Stat(absolute); err != nil {
			return fmt.Sprintf("the module wrapper %s is not built: run mage test:endToEndZarf, "+
				"which builds both", wrapper.path)
		}
		*wrapper.into = absolute
	}
	env.publicKey = filepath.Join(env.root, packagePublicKey)

	for _, cluster := range []string{bareCluster, storedCluster} {
		if clusterExists(cluster) {
			// Refusing rather than adopting. The suite deletes the clusters it created, and a
			// cluster somebody else is using is not one to delete on their behalf.
			return fmt.Sprintf("a k3d cluster named %q already exists; delete it with "+
				"`k3d cluster delete %s` before running this suite", cluster, cluster)
		}
	}

	if reason := resolvePackages(env); reason != "" {
		return reason
	}

	collections, err := stageCollections(env.root)
	if err != nil {
		return fmt.Sprintf("unable to stage the collections path: %v", err)
	}
	env.collections = collections

	return ""
}

// zarfReleaseVersion is the version of the zarf on PATH, as the tag of the matching init package.
func zarfReleaseVersion(zarfPath string) (string, error) {
	out, err := exec.Command(zarfPath, "version").Output()
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(out))
	if version == "" {
		return "", fmt.Errorf("`zarf version` printed nothing")
	}
	// The init package is tagged with a leading v whether or not `zarf version` prints one.
	return "v" + strings.TrimPrefix(version, "v"), nil
}

// resolvePackages finds or pulls the two packages the walks deploy.
//
// The init package has to match the zarf that deploys it -- zarf looks for an init package named
// after its own version in the working directory -- so its tag is the installed zarf's version
// rather than a pin in this file, which would go stale the first time anybody upgraded zarf. The
// storage provider is pinned, because it is the uds-bundle's package and nothing about it follows
// the zarf release.
func resolvePackages(env *environment) string {
	dir := filepath.Join(env.root, packageCache)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Sprintf("unable to create %s: %v", dir, err)
	}

	initName := fmt.Sprintf("zarf-init-%s-%s.tar.zst", env.arch, env.zarfVersion)
	initRef := "oci://ghcr.io/zarf-dev/packages/init:" + env.zarfVersion
	pkg, reason := havePackage(env, dir, initName, initRef, envInitPackage)
	if reason != "" {
		return reason
	}
	env.initPackage = pkg

	storageName := fmt.Sprintf(storagePackageFile, env.arch)
	pkg, reason = havePackage(env, dir, storageName, storagePackage, envStoragePackage)
	if reason != "" {
		return reason
	}
	env.storagePackage = pkg

	return ""
}

// havePackage returns the package at name inside dir, pulling ref when it is not already there,
// and why it could not be had. The environment variable wins over both.
func havePackage(env *environment, dir, name, ref, envName string) (string, string) {
	if named := os.Getenv(envName); named != "" {
		if _, err := os.Stat(named); err != nil {
			return "", fmt.Sprintf("%s names %s, which cannot be read: %v", envName, named, err)
		}
		return named, ""
	}

	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return path, ""
	}

	log.Printf("pulling %s into %s", ref, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, env.zarf, "package", "pull", ref,
		"--architecture", env.arch, "--output-directory", dir, "--no-color")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Sprintf("unable to pull %s: %v; set %s to a package already on disk",
			ref, err, envName)
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Sprintf("the pull of %s did not produce %s", ref, path)
	}
	return path, ""
}

// stageCollections builds the directory Ansible wants: a collections path holding an
// ansible_collections directory whose children are the namespaces.
//
// The repository's tree is ansible/colonel_byte/<collection>, which is the same shape one level
// down, so a symlink named ansible_collections pointing at ansible/ is the whole of it. It is a
// symlink rather than a copy so that a run exercises the working tree: editing an action plugin
// and running the suite again has to pick the edit up.
func stageCollections(root string) (string, error) {
	dir, err := os.MkdirTemp("", "cargoship-zarf-e2e-collections-")
	if err != nil {
		return "", err
	}
	if err := os.Symlink(filepath.Join(root, "ansible"), filepath.Join(dir, "ansible_collections")); err != nil {
		return "", err
	}
	return dir, nil
}

// cluster is a k3d cluster the suite created, and the kubeconfig that reaches it.
type cluster struct {
	name       string
	kubeconfig string
}

// withCluster creates the cluster the given fixture describes and removes it when the test ends.
//
// Both k3d configs ask k3d to write the cluster into the default kubeconfig and switch to it. A
// test suite has no business doing either, so both are overridden on the command line and the
// kubeconfig is written somewhere of its own. The fixtures stay as the upstream file.
func withCluster(t *testing.T, config, name string) *cluster {
	t.Helper()

	if err := os.MkdirAll(localPathVolume, 0o755); err != nil {
		t.Fatalf("unable to create the bind source %s: %v", localPathVolume, err)
	}

	t.Logf("creating the k3d cluster %q from %s", name, config)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	create := exec.CommandContext(ctx, "k3d", "cluster", "create",
		"--config", filepath.Join(suite.root, config),
		"--kubeconfig-update-default=false",
		"--kubeconfig-switch-context=false",
		"--wait")
	create.Stdout = os.Stderr
	create.Stderr = os.Stderr
	if err := create.Run(); err != nil {
		t.Fatalf("unable to create the cluster: %v", err)
	}

	t.Cleanup(func() {
		if os.Getenv(envKeep) != "" {
			t.Logf("%s is set, leaving the cluster %q in place", envKeep, name)
			return
		}
		deleteCtx, deleteCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer deleteCancel()
		del := exec.CommandContext(deleteCtx, "k3d", "cluster", "delete", name)
		del.Stdout = os.Stderr
		del.Stderr = os.Stderr
		if err := del.Run(); err != nil {
			t.Errorf("unable to delete the cluster %q: %v", name, err)
		}
	})

	out, err := exec.CommandContext(ctx, "k3d", "kubeconfig", "get", name).Output()
	if err != nil {
		t.Fatalf("unable to read the cluster's kubeconfig: %v", err)
	}
	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(kubeconfig, out, 0o600); err != nil {
		t.Fatalf("unable to write %s: %v", kubeconfig, err)
	}

	// One server and one agent, per both fixtures. Waiting for both matters because the registry
	// the initialisation deploys can be scheduled onto either.
	clientset := kubeClient(t, kubeconfig)
	if err := test.WaitForNodesReady(ctx, clientset, 2, 5*time.Minute); err != nil {
		t.Fatalf("the cluster's nodes did not become ready: %v", err)
	}

	return &cluster{name: name, kubeconfig: kubeconfig}
}

func clusterExists(name string) bool {
	out, err := exec.Command("k3d", "cluster", "list", name, "--no-headers").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func kubeClient(t *testing.T, kubeconfig string) *kubernetes.Clientset {
	t.Helper()
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatalf("unable to build a client for %s: %v", kubeconfig, err)
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("unable to build a client for %s: %v", kubeconfig, err)
	}
	return clientset
}

// requireSuite skips the calling test when a prerequisite was missing, naming the one that was.
func requireSuite(t *testing.T) {
	t.Helper()
	// Checked here rather than in prepare: testing.Short reads a flag, and flags are not parsed
	// until m.Run, which is after TestMain has called prepare.
	if testing.Short() {
		t.Skip("the suite needs k3d clusters, which -short excludes")
	}
	if suite.skip != "" {
		t.Skipf("the zarf module e2e suite cannot run: %s", suite.skip)
	}
}

// walk runs one of the playbooks against a cluster and returns its output and the directory the
// per-step results were written to.
func (c *cluster) walk(t *testing.T, playbook string) (string, string) {
	t.Helper()

	results := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, suite.ansible, filepath.Join(suite.root, playbook),
		"-v", // the progress the action plugins display is verbose-level output
		"-i", "localhost,",
		"-e", "zarf_init_wrapper="+suite.initWrapper,
		"-e", "zarf_deploy_wrapper="+suite.deployWrapper,
		"-e", "zarf_init_package="+suite.initPackage,
		"-e", "zarf_storage_package="+suite.storagePackage,
		"-e", "zarf_public_key="+suite.publicKey,
		"-e", "zarf_kubeconfig="+c.kubeconfig,
		"-e", "zarf_result_dir="+results,
		"-e", "zarf_timeout=15m",
	)
	cmd.Env = append(os.Environ(),
		"ANSIBLE_COLLECTIONS_PATH="+suite.collections,
		// A play over localhost with no inventory warns, and the warning is noise in a failure.
		"ANSIBLE_LOCALHOST_WARNING=False",
		"ANSIBLE_INVENTORY_UNPARSED_WARNING=False",
		// The suite reads the progress lines out of the output, so they have to stay unstyled.
		"ANSIBLE_FORCE_COLOR=0",
		"NO_COLOR=1",
	)

	out, runErr := cmd.CombinedOutput()
	t.Logf("ansible-playbook %s:\n%s", filepath.Base(playbook), out)
	if runErr != nil {
		t.Fatalf("the playbook failed: %v", runErr)
	}
	return string(out), results
}

// runWrapper invokes a module wrapper the way the action plugin does: the action in
// ZARF_ANSIBLE_MODULE, the parameters on stdin, and one JSON object on stdout.
//
// It returns the decoded result and the module's stderr, which is where zarf's own output went.
func runWrapper(t *testing.T, wrapper, module string, params map[string]any) (map[string]any, string) {
	t.Helper()

	payload, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("unable to encode the module parameters: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	var stdout, stderr strings.Builder
	cmd := exec.CommandContext(ctx, wrapper)
	cmd.Env = append(os.Environ(), "ZARF_ANSIBLE_MODULE="+module)
	cmd.Stdin = strings.NewReader(string(payload))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// A module reports failure inside its result and still exits 0. A non-zero status here is the
	// wrapper itself breaking, which is a different bug and worth a different message.
	if err := cmd.Run(); err != nil {
		t.Fatalf("the wrapper exited non-zero (%v); stdout %q stderr %q",
			err, stdout.String(), stderr.String())
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("the wrapper did not write one JSON object: %v; stdout %q", err, stdout.String())
	}
	return result, stderr.String()
}

// readStep reads one step's registered result.
func readStep(t *testing.T, results, name string) map[string]any {
	t.Helper()
	path := filepath.Join(results, name)
	data, err := os.ReadFile(path) //nolint:gosec // the path is the test's own temporary directory
	if err != nil {
		t.Fatalf("unable to read the registered result %s: %v", name, err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("the registered result %s is not JSON: %v (%s)", name, err, data)
	}
	return result
}
