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
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// flagList is the recorded zarf init flag surface. See testdata/zarf-init-flags.txt.
const flagList = "testdata/zarf-init-flags.txt"

// fakeRunner stands in for the installed zarf.
type fakeRunner struct {
	lines    []string
	exitCode int
	err      error

	got Invocation
}

func (f *fakeRunner) Run(_ context.Context, in Invocation) (Result, error) {
	f.got = in
	for _, line := range f.lines {
		if in.OnLine != nil {
			in.OnLine(line)
		}
	}
	return Result{ExitCode: f.exitCode}, f.err
}

// argsFor writes an arguments file holding params and reads it back the way a WANT_JSON caller
// delivers it.
func argsFor(t *testing.T, params map[string]any) *Args {
	t.Helper()
	b, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("unable to encode the test parameters: %v", err)
	}
	path := filepath.Join(t.TempDir(), "args")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("unable to write the test arguments file: %v", err)
	}
	args, err := ReadArgsFile(path)
	if err != nil {
		t.Fatalf("unable to read the test arguments file: %v", err)
	}
	return args
}

func TestModuleName(t *testing.T) {
	tests := []struct {
		name  string
		argv0 string
		env   string
		want  string
		ok    bool
	}{
		{
			name:  "module symlink",
			argv0: "/usr/share/ansible/collections/ansible_collections/colonel_byte/zarf/plugins/modules/zarf_init",
			want:  "init",
			ok:    true,
		},
		{
			name:  "staged copy carries the AnsiballZ prefix",
			argv0: "/root/.ansible/tmp/ansible-tmp-1/AnsiballZ_zarf_init",
			want:  "init",
			ok:    true,
		},
		{
			name:  "the environment wins",
			argv0: "/usr/local/bin/zarf-ansible",
			env:   "init",
			want:  "init",
			ok:    true,
		},
		{
			name:  "a build artifact carrying the prefix is not a module",
			argv0: "/build/zarf_linux_amd64",
			ok:    false,
		},
		{
			name:  "the deploy module symlink",
			argv0: "/usr/bin/zarf_package_deploy",
			want:  "package_deploy",
			ok:    true,
		},
		{
			name:  "an action this wrapper does not answer as is not a module",
			argv0: "/usr/bin/zarf_package_remove",
			ok:    false,
		},
		{
			name:  "the plain binary is not a module",
			argv0: "/usr/bin/zarf",
			ok:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvModule, tt.env)

			got, ok := ModuleName(tt.argv0)
			if ok != tt.ok {
				t.Fatalf("ModuleName(%q) reported %v, want %v", tt.argv0, ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("ModuleName(%q) = %q, want %q", tt.argv0, got, tt.want)
			}
		})
	}
}

func TestBuildInitArgs(t *testing.T) {
	ownership := true
	plainHTTP := false
	port := 31999

	tests := []struct {
		name   string
		params initParams
		want   []string
	}{
		{
			name:   "the flags every run renders",
			params: initParams{},
			want: []string{
				"init",
				"--confirm",
				"--no-color",
				"--log-format", "json",
			},
		},
		{
			name: "an overridden log format is not replaced",
			params: initParams{
				LogFormat: "console",
				LogLevel:  "debug",
			},
			want: []string{
				"init",
				"--confirm",
				"--no-color",
				"--log-format", "console",
				"--log-level", "debug",
			},
		},
		{
			name: "a false boolean renders, an unset one does not",
			params: initParams{
				TakeOwnership: &ownership,
				PlainHTTP:     &plainHTTP,
			},
			want: []string{
				"init",
				"--confirm",
				"--no-color",
				"--log-format", "json",
				"--take-ownership=true",
				"--plain-http=false",
			},
		},
		{
			name: "registry and git credentials",
			params: initParams{
				RegistryURL:          "registry.bubbles.test:5000",
				RegistryPort:         &port,
				RegistryPushUsername: "zarf-push",
				RegistryPushPassword: "hunter2",
				GitURL:               "https://git.bubbles.test",
				GitPushPassword:      "hunter3",
			},
			want: []string{
				"init",
				"--confirm",
				"--no-color",
				"--log-format", "json",
				"--registry-url", "registry.bubbles.test:5000",
				"--registry-port", "31999",
				"--registry-push-username", "zarf-push",
				"--registry-push-password", "hunter2",
				"--git-url", "https://git.bubbles.test",
				"--git-push-password", "hunter3",
			},
		},
		{
			name: "repeated and map flags render one copy per entry, sorted",
			params: initParams{
				Values: []string{"/etc/zarf/a.yaml", "/etc/zarf/b.yaml"},
				SetValues: map[string]string{
					"zebra":   "stripes",
					"leopard": "spots",
				},
				SetVariables: map[string]string{
					"DOMAIN": "bubbles.test",
				},
			},
			want: []string{
				"init",
				"--confirm",
				"--no-color",
				"--log-format", "json",
				"--values", "/etc/zarf/a.yaml",
				"--values", "/etc/zarf/b.yaml",
				"--set-values", "leopard=spots",
				"--set-values", "zebra=stripes",
				"--set-variables", "DOMAIN=bubbles.test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildInitArgs(&tt.params)
			if strings.Join(got, " ") != strings.Join(tt.want, " ") {
				t.Errorf("buildInitArgs rendered\n  %v\nwant\n  %v", got, tt.want)
			}
		})
	}
}

// TestInitArgsAreKnownFlags holds every flag the module can render against the recorded zarf init
// flag surface. A flag the module renders and zarf does not accept fails the run with a usage
// error, which reads as the module being broken rather than as a parameter being wrong.
func TestInitArgsAreKnownFlags(t *testing.T) {
	known := readKnownFlags(t, flagList)

	// Every parameter set, so that every flag the module can render is in one vector.
	yes := true
	number := 1
	widest := initParams{
		Components:            "zarf-registry",
		StorageClass:          "local-path",
		RegistryURL:           "registry.bubbles.test",
		RegistryMode:          "internal",
		RegistryPort:          &number,
		RegistrySecret:        "secret",
		RegistryPushUsername:  "push",
		RegistryPushPassword:  "push",
		RegistryPullUsername:  "pull",
		RegistryPullPassword:  "pull",
		GitURL:                "https://git.bubbles.test",
		GitPushUsername:       "push",
		GitPushPassword:       "push",
		GitPullUsername:       "pull",
		GitPullPassword:       "pull",
		InjectorImage:         "registry.bubbles.test/injector:1",
		InjectorPort:          &number,
		AgentMutationPolicy:   "all",
		AgentTLSCA:            "/etc/zarf/ca.pem",
		AgentTLSCert:          "/etc/zarf/tls.pem",
		AgentTLSKey:           "/etc/zarf/tls.key",
		TakeOwnership:         &yes,
		ForceConflicts:        &yes,
		SkipValuesSchemaValid: &yes,
		InsecureSkipTLSVerify: &yes,
		PlainHTTP:             &yes,
		Retries:               &number,
		OCIConcurrency:        &number,
		Architecture:          "amd64",
		Cache:                 "/var/cache/zarf",
		Tmpdir:                "/var/tmp/zarf",
		Timeout:               "30m",
		PublicKey:             "/etc/zarf/cosign.pub",
		Verify:                "always",
		LogLevel:              "debug",
		LogFormat:             "json",
		Values:                []string{"/etc/zarf/values.yaml"},
		SetValues:             map[string]string{"key": "value"},
		SetVariables:          map[string]string{"KEY": "value"},
	}

	for _, arg := range buildInitArgs(&widest) {
		name, ok := strings.CutPrefix(arg, "--")
		if !ok {
			continue
		}
		name, _, _ = strings.Cut(name, "=")
		if !known[name] {
			t.Errorf("the module renders --%s, which %s does not list", name, flagList)
		}
	}
}

func readKnownFlags(t *testing.T, path string) map[string]bool {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // the path is a fixture in this package
	if err != nil {
		t.Fatalf("unable to read the recorded zarf flag list: %v", err)
	}
	defer f.Close() //nolint:errcheck // read-only

	known := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		known[line] = true
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("unable to read the recorded zarf flag list: %v", err)
	}
	return known
}

func TestRedact(t *testing.T) {
	argv := []string{
		"init",
		"--registry-push-password", "hunter2",
		"--git-pull-password", "hunter3",
		"--registry-url", "registry.bubbles.test",
	}
	want := []string{
		"init",
		"--registry-push-password", "<redacted>",
		"--git-pull-password", "<redacted>",
		"--registry-url", "registry.bubbles.test",
	}

	got := redact(argv)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("redact returned\n  %v\nwant\n  %v", got, want)
	}
}

func TestComponentName(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
		ok   bool
	}{
		{
			name: "a JSON record naming the component",
			line: `{"time":"2026-10-02T14:32:01Z","level":"INFO","msg":"Deploying component","name":"zarf-registry"}`,
			want: "zarf-registry",
			ok:   true,
		},
		{
			name: "a JSON record carrying the name under another key",
			line: `{"level":"INFO","msg":"deploying component","component":"zarf-agent"}`,
			want: "zarf-agent",
			ok:   true,
		},
		{
			name: "a JSON record that names no component falls back to the message",
			line: `{"level":"INFO","msg":"Deploying component"}`,
			want: "Deploying component",
			ok:   true,
		},
		{
			name: "the console rendering",
			line: `  •  Deploying Zarf component - zarf-seed-registry`,
			want: "zarf-seed-registry",
			ok:   true,
		},
		{
			name: "an unrelated record",
			line: `{"level":"INFO","msg":"Loading Zarf Package"}`,
			ok:   false,
		},
		{
			name: "an unrelated line",
			line: `  •  Waiting for the registry to be ready`,
			ok:   false,
		},
		{
			name: "a blank line",
			line: "   ",
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := componentName(tt.line)
			if ok != tt.ok {
				t.Fatalf("componentName(%q) reported %v, want %v", tt.line, ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("componentName(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

// TestProgressWritesHeartbeats proves the side channel: every component boundary the log stream
// announces leaves a whole, parseable status document in place.
func TestProgressWritesHeartbeats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	hb := NewHeartbeat(path)
	prog := newProgress(hb, 2)

	prog.line(`{"msg":"Deploying component","name":"zarf-injector"}`)
	first := readStatus(t, path)
	if first.Phase != "zarf-injector" || first.Index != 1 || first.Total != 2 || first.Status != "running" {
		t.Fatalf("the first heartbeat was %+v", first)
	}

	// A repeat of the same component is not a new one.
	prog.line(`{"msg":"Deploying component","name":"zarf-injector"}`)
	if again := readStatus(t, path); again.Index != 1 {
		t.Errorf("a repeated announcement advanced the index to %d", again.Index)
	}

	prog.line(`{"msg":"Deploying component","name":"zarf-registry"}`)
	second := readStatus(t, path)
	if second.Phase != "zarf-registry" || second.Index != 2 || second.Status != "running" {
		t.Fatalf("the second heartbeat was %+v", second)
	}

	prog.finish("completed", nil)
	done := readStatus(t, path)
	if done.Status != "completed" || done.Phase != "zarf-registry" {
		t.Errorf("the final heartbeat was %+v", done)
	}

	hb.Clear()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the status file survived Clear: %v", err)
	}
}

func readStatus(t *testing.T, path string) Status {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // the path is the test's own temporary directory
	if err != nil {
		t.Fatalf("unable to read the status file: %v", err)
	}
	var st Status
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("the status file did not hold one JSON document: %v (%s)", err, b)
	}
	return st
}

func TestRunInitCheckModeSkips(t *testing.T) {
	args := argsFor(t, map[string]any{
		"init_package":        "/srv/staging/zarf-init-amd64-v0.70.0.tar.zst",
		"_ansible_check_mode": true,
	})
	resp := &Response{Zarf: &Detail{Module: "init"}}
	run := &fakeRunner{}

	if err := runInit(context.Background(), run, args, resp); err != nil {
		t.Fatalf("runInit reported %v", err)
	}
	if !resp.Skipped {
		t.Error("a check-mode run was not reported as skipped")
	}
	if resp.Changed {
		t.Error("a check-mode run reported changed")
	}
	if run.got.Binary != "" {
		t.Errorf("a check-mode run executed %q", run.got.Binary)
	}
	if resp.Zarf.ChangedSignal != SignalUnknown {
		t.Errorf("the changed signal was %q, want %q", resp.Zarf.ChangedSignal, SignalUnknown)
	}
}

func TestRunInitReportsTheRun(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "zarf-init-amd64-v0.70.0.tar.zst")
	if err := os.WriteFile(pkg, []byte("not really a package"), 0o600); err != nil {
		t.Fatalf("unable to stage the test init package: %v", err)
	}

	args := argsFor(t, map[string]any{
		"init_package":           pkg,
		"zarf_binary":            "/usr/bin/zarf",
		"kubeconfig":             "/etc/rancher/rke2/rke2.yaml",
		"registry_push_password": "hunter2",
		"status_file":            filepath.Join(dir, "status.json"),
	})
	resp := &Response{Zarf: &Detail{Module: "init"}}
	run := &fakeRunner{
		lines: []string{
			`{"msg":"Loading Zarf Package"}`,
			`{"msg":"Deploying component","name":"zarf-injector"}`,
			`{"msg":"Deploying component","name":"zarf-seed-registry"}`,
		},
	}

	if err := runInit(context.Background(), run, args, resp); err != nil {
		t.Fatalf("runInit reported %v", err)
	}

	if run.got.Dir != dir {
		t.Errorf("zarf ran in %q, want the init package's directory %q", run.got.Dir, dir)
	}
	if run.got.Binary != "/usr/bin/zarf" {
		t.Errorf("zarf was run as %q", run.got.Binary)
	}
	if !containsEnv(run.got.Env, "KUBECONFIG=/etc/rancher/rke2/rke2.yaml") {
		t.Errorf("the child environment was %v, want it to carry KUBECONFIG", run.got.Env)
	}
	if got, want := strings.Join(resp.Zarf.ComponentsRan, ","), "zarf-injector,zarf-seed-registry"; got != want {
		t.Errorf("componentsRan was %q, want %q", got, want)
	}
	if !resp.Changed || resp.Zarf.ChangedSignal != SignalPartial {
		t.Errorf("the run reported changed=%v signal=%q", resp.Changed, resp.Zarf.ChangedSignal)
	}
	if strings.Contains(strings.Join(resp.Zarf.Command, " "), "hunter2") {
		t.Errorf("the reported command line carried a password: %v", resp.Zarf.Command)
	}
}

func TestRunInitRequiresAPackageOrADirectory(t *testing.T) {
	args := argsFor(t, map[string]any{})
	resp := &Response{Zarf: &Detail{Module: "init"}}

	err := runInit(context.Background(), &fakeRunner{}, args, resp)
	if err == nil {
		t.Fatal("runInit accepted parameters naming neither a package nor a directory")
	}
	if !strings.Contains(err.Error(), "init_package") {
		t.Errorf("the error did not name the missing parameter: %v", err)
	}
}

func TestUnknownParameterIsNamed(t *testing.T) {
	args := argsFor(t, map[string]any{
		"init_package":  "/srv/staging/zarf-init-amd64.tar.zst",
		"registry_urll": "registry.bubbles.test",
	})

	var p initParams
	err := args.Params(&p)
	if err == nil {
		t.Fatal("a misspelled parameter was accepted")
	}
	if !strings.Contains(err.Error(), "registry_urll") {
		t.Errorf("the error did not name the misspelling: %v", err)
	}
}

func TestReadArgsFromStdin(t *testing.T) {
	args, err := ReadArgs(strings.NewReader(
		`{"init_package":"/srv/staging/zarf-init-amd64.tar.zst","_ansible_verbosity":2}`))
	if err != nil {
		t.Fatalf("ReadArgs reported %v", err)
	}
	if args.Control.Verbosity != 2 {
		t.Errorf("verbosity was %d, want 2", args.Control.Verbosity)
	}

	var p initParams
	if err := args.Params(&p); err != nil {
		t.Fatalf("Params reported %v", err)
	}
	if p.InitPackage != "/srv/staging/zarf-init-amd64.tar.zst" {
		t.Errorf("init_package was %q", p.InitPackage)
	}
}

func TestResponseIsOneJSONObject(t *testing.T) {
	var out strings.Builder
	resp := &Response{
		Changed: true,
		Msg:     "initialised the cluster",
		Zarf: &Detail{
			Module:        "init",
			ComponentsRan: []string{"zarf-injector"},
			ChangedSignal: SignalPartial,
		},
	}
	if err := resp.emit(&out); err != nil {
		t.Fatalf("emit reported %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out.String()), &decoded); err != nil {
		t.Fatalf("the response was not one JSON object: %v (%s)", err, out.String())
	}
	if _, ok := decoded["zarf"]; !ok {
		t.Errorf("the response carried no zarf detail: %s", out.String())
	}
}

func containsEnv(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
