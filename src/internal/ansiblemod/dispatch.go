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

// Package ansiblemod lets the cargoship binary answer Ansible as a module.
//
// There is no second binary and no Python wrapper around this one. The binary presents itself as
// several module files by looking at the name it was invoked under: a basename of cargoship_
// followed by the name of an action it answers as enters module mode, and anything else is the
// ordinary CLI. The module files are symlinks to the binary, so there is nothing extra to sign, publish, or
// carry through an airlock, and the thing answering Ansible is the thing doing the work.
//
// A module here does not reimplement a command. It turns the module's JSON parameters into the
// argument vector an operator would have typed and runs that, so package loading, keyring
// resolution, timeouts, and phase construction stay on one path.
//
// See docs/agent/choice-ansible-module.md.
package ansiblemod

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// Prefix is the basename prefix that puts the binary into module mode.
	Prefix = "cargoship_"
	// EnvModule selects a module without a symlink. Tests have no reason to create symlinks to
	// check the contract, and neither does an operator reproducing a module run by hand.
	EnvModule = "CARGOSHIP_ANSIBLE_MODULE"
)

// Exec runs a cargoship command line, as src/cmd.ExecuteArgs does.
//
// It is injected rather than imported because src/cmd imports this package to reach ModuleName and
// Run, and a package cannot import the package that imports it. What the indirection costs is that
// the flag names below are written out here instead of referenced from src/cmd; what keeps them
// true is TestModuleArgsParse in src/cmd, which parses each module's widest argument vector against
// the real command.
type Exec func(ctx context.Context, argv []string) error

// action runs one module. It reports what happened through resp and returns an error only for a
// failure Ansible should see as one.
type action func(ctx context.Context, exec Exec, args *Args, resp *Response) error

// modules is every action this binary answers as.
//
// These are the actions that converge a fleet, which is what a playbook is for. Package creation
// is deliberately absent: it builds an artifact on one machine from a definition in a repository,
// which is a build step rather than a convergence, and Ansible has nothing to offer it that a
// pipeline does not already do better.
var modules = map[string]action{
	"apply":              runApply,
	"engine_config_sync": runEngineConfigSync,
	"kube_config":        runKubeConfig,
	"prepare":            runPrepare,
	"reset":              runReset,
}

// Modules returns the actions this binary answers as, sorted.
func Modules() []string {
	out := make([]string, 0, len(modules))
	for name := range modules {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ModuleName returns the module this process was invoked as, and whether it was invoked as one at
// all. The environment variable wins, so a test can drive module mode through the binary it
// already builds.
//
// The name has to be one of the actions above, not merely carry the prefix. The prefix alone is
// not rare enough to dispatch on: the repository builds its own binary as cargoship_linux_amd64,
// every e2e suite runs that file, and an operator who keeps two versions side by side names them
// something similar. Treating those as modules turns an ordinary command into a JSON object
// nobody asked for. The cost is that a misspelled symlink runs the CLI instead of saying it is
// not a module, which fails on the next line rather than this one.
func ModuleName(argv0 string) (string, bool) {
	if name := os.Getenv(EnvModule); name != "" {
		return name, true
	}
	base := filepath.Base(argv0)
	name, ok := strings.CutPrefix(base, Prefix)
	if !ok {
		return "", false
	}
	if _, answers := modules[name]; !answers {
		return "", false
	}
	return name, true
}

// Run executes module mode and returns the process exit status.
//
// argv is the process argument vector. Ansible invokes a WANT_JSON module with one argument, the
// path of a file holding the parameters as JSON.
func Run(ctx context.Context, name string, argv []string, exec Exec) int {
	guard, err := newStdoutGuard()
	if err != nil {
		// Nothing can be reported as a module result, because there is no channel to report it
		// on. Say so where a human will see it and fail.
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer guard.Close() //nolint:errcheck // the process is about to exit

	resp := &Response{
		Cargoship: &Detail{Module: name, ChangedSignal: SignalUnknown},
	}
	if err := dispatch(ctx, exec, name, argv, resp); err != nil {
		resp.fail(err)
	}

	if err := resp.emit(guard); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// Exit 0 whatever happened. A module reports failure in the failed field; a non-zero status
	// makes Ansible report "MODULE FAILURE" and show its own diagnosis instead of the message
	// the module wrote, which is the less useful of the two.
	return 0
}

func dispatch(ctx context.Context, exec Exec, name string, argv []string, resp *Response) error {
	run, ok := modules[name]
	if !ok {
		return fmt.Errorf("%q is not a cargoship Ansible module; this binary answers as %s",
			Prefix+name, quoteAll(Modules()))
	}

	if len(argv) < 2 {
		return errors.New("no arguments file: this module follows the WANT_JSON convention, " +
			"so Ansible passes the parameters as the path of a JSON file")
	}

	args, err := ReadArgs(argv[1])
	if err != nil {
		return err
	}
	resp.Cargoship.CheckMode = args.Control.CheckMode

	return run(ctx, exec, args, resp)
}
