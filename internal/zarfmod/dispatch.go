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

// Package zarfmod lets a small wrapper binary answer Ansible as a zarf module.
//
// This is a proof of ZEP-0072 (https://github.com/zarf-dev/proposals/pull/73) built where the
// pattern it copies already exists. It differs from that proposal in one way that matters: the
// module here is not the zarf binary. ZEP-0072 proposes putting module dispatch inside zarf
// itself, which this repository cannot do, so each module file is a separate wrapper binary
// (cmd/zarf/init, cmd/zarf/deploy) that renders the zarf command line, runs the installed zarf,
// and reports the run. Everything else -- argv[0] dispatch, the WANT_JSON intake, the stdout guard, the single
// JSON response, the heartbeat side channel -- is the shape the proposal describes, and is modeled
// on internal/ansiblemod. See docs/agent/choice-zarf-ansible-module.md.
//
// A wrapper does not reimplement the command it drives. It turns the module's JSON parameters into
// the argument vector an operator would have typed and runs that, so component selection,
// credential handling, signature verification, and timeouts stay on zarf's own path.
package zarfmod

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
	// Prefix is the basename prefix that puts a wrapper binary into module mode.
	Prefix = "zarf_"
	// AnsiballZ is the prefix Ansible puts on a module file when it copies it to the machine that
	// runs it. The copy is what executes, so the name the process sees is AnsiballZ_zarf_init
	// rather than the zarf_init the collection holds.
	AnsiballZ = "AnsiballZ_"
	// EnvModule selects a module without a symlink. It is what the action plugin sets when it
	// invokes the wrapper directly, and what a test or an operator reproducing a run by hand uses.
	EnvModule = "ZARF_ANSIBLE_MODULE"
)

// action runs one module. It reports what happened through resp and returns an error only for a
// failure Ansible should see as one.
type action func(ctx context.Context, run Runner, args *Args, resp *Response) error

// modules is every action this wrapper answers as.
//
// init and package_deploy, because neither is useful alone: an init package that disables k3s
// local-storage has no StorageClass for the registry to claim until a storage provider is
// deployed, and a provider cannot be deployed onto a cluster that has not been initialised. The
// two-stage walk in test/e2e/zarf is what proves that pair. ZEP-0072 also names package_remove,
// which nothing here needs yet.
var modules = map[string]action{
	"init":           runInit,
	"package_deploy": runPackageDeploy,
}

// Modules returns the actions this wrapper answers as, sorted.
func Modules() []string {
	out := make([]string, 0, len(modules))
	for name := range modules {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ModuleName returns the module this process was invoked as, and whether it was invoked as one at
// all. The environment variable wins, so the action plugin can invoke the installed wrapper
// without a symlink and a test can drive module mode through the binary it already builds.
//
// The name has to be one of the actions above, not merely carry the prefix. A build artifact named
// zarf_linux_amd64 carries the prefix and is not a module, and answering it with JSON nobody asked
// for is worse than the cost of this check: a misspelled symlink reports "not a module" instead of
// naming the misspelling.
func ModuleName(argv0 string) (string, bool) {
	if name := os.Getenv(EnvModule); name != "" {
		return name, true
	}
	base := strings.TrimPrefix(filepath.Base(argv0), AnsiballZ)
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
func Run(ctx context.Context, name string, argv []string, run Runner) int {
	guard, err := newStdoutGuard()
	if err != nil {
		// Nothing can be reported as a module result, because there is no channel to report it
		// on. Say so where a human will see it and fail.
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer guard.Close() //nolint:errcheck // the process is about to exit

	resp := &Response{Zarf: &Detail{Module: name}}
	if err := dispatch(ctx, run, name, argv, resp); err != nil {
		resp.fail(err)
	}

	if err := resp.emit(guard); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// Exit 0 whatever happened. A module reports failure in the failed field; a non-zero status
	// makes Ansible report "MODULE FAILURE" and show its own diagnosis instead of the message the
	// module wrote, which is the less useful of the two.
	return 0
}

func dispatch(ctx context.Context, run Runner, name string, argv []string, resp *Response) error {
	do, ok := modules[name]
	if !ok {
		return fmt.Errorf("%q is not a zarf Ansible module; this binary answers as %s",
			Prefix+name, quoteAll(Modules()))
	}

	args, err := readDispatchArgs(argv)
	if err != nil {
		return err
	}
	resp.Zarf.CheckMode = args.Control.CheckMode

	return do(ctx, run, args, resp)
}

// readDispatchArgs reads the module parameters from wherever this invocation delivers them.
//
// Both intake conventions ZEP-0072 weighs are implemented, because the wrapper has to answer two
// callers. Ansible's own module staging passes the path of a JSON file as argv[1], which is the
// WANT_JSON contract and the only thing a third-party playbook calling the symlink can do. The
// action plugin in this collection bypasses that staging and pipes the same JSON on stdin, which
// keeps registry and git credentials off disk for the path an operator actually runs. The file
// path wins when both are available, so a WANT_JSON caller is never second-guessed.
func readDispatchArgs(argv []string) (*Args, error) {
	if len(argv) >= 2 && argv[1] != "" && argv[1] != "-" {
		return ReadArgsFile(argv[1])
	}
	if stdinIsPipe() {
		return ReadArgs(os.Stdin)
	}
	return nil, errors.New("no module parameters: pass the path of a JSON arguments file as the " +
		"single argument, as the WANT_JSON convention does, or pipe the same JSON on stdin")
}

// stdinIsPipe reports whether stdin is something a caller wrote to rather than a terminal. A
// module invoked by hand with no arguments should be told what it wants, not left blocking on a
// read that will never return.
func stdinIsPipe() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}
