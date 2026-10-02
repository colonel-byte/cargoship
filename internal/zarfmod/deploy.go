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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The flags of zarf package deploy the module renders, beyond the ones it shares with init. They
// are held against testdata/zarf-package-deploy-flags.txt by TestDeployArgsAreKnownFlags, for the
// reason given on the init flag block.
const (
	flagConnected = "connected"
	flagNamespace = "namespace"
	flagShasum    = "shasum"
)

// deployParams is the zarf_package_deploy module's parameter surface.
//
// A deploy is what follows an init: the cluster exists, zarf's state is in it, and this puts a
// package on it. The parameters are therefore nearly the init set minus everything that configures
// the registry and git server, plus the package to deploy.
type deployParams struct {
	// Binary is the zarf to run. The wrapper is a module file, not zarf.
	Binary string `json:"zarf_binary"`
	// Package is what to deploy: a path to a tarball, an oci:// reference, or an https:// URL.
	// It is the positional argument of the command.
	Package string `json:"package"`
	// Directory is the working directory to run zarf in. A relative package path is resolved
	// against it, and a package already named absolutely needs none of this.
	Directory string `json:"directory"`
	// Kubeconfig is the cluster to deploy onto. Zarf has no --kubeconfig flag, so it is passed as
	// KUBECONFIG in the child's environment.
	Kubeconfig string `json:"kubeconfig"`
	// ZarfConfig is an operator-supplied zarf config file, passed as ZARF_CONFIG.
	ZarfConfig string `json:"zarf_config"`
	// StatusFile overrides the heartbeat file; the action plugin sets ZARF_STATUS_FILE instead.
	StatusFile string `json:"status_file"`

	Components string `json:"components"`
	Namespace  string `json:"namespace"`
	Shasum     string `json:"shasum"`

	Connected             *bool `json:"connected"`
	ForceConflicts        *bool `json:"force_conflicts"`
	SkipValuesSchemaValid *bool `json:"skip_values_schema_validation"`
	InsecureSkipTLSVerify *bool `json:"insecure_skip_tls_verify"`
	PlainHTTP             *bool `json:"plain_http"`
	TakeOwnership         *bool `json:"take_ownership"`

	Retries        *int   `json:"retries"`
	OCIConcurrency *int   `json:"oci_concurrency"`
	Architecture   string `json:"architecture"`
	Cache          string `json:"cache"`
	Tmpdir         string `json:"tmpdir"`
	Timeout        string `json:"timeout"`
	PublicKey      string `json:"public_key"`
	Verify         string `json:"verify"`

	Values       []string          `json:"values"`
	SetValues    map[string]string `json:"set_values"`
	SetVariables map[string]string `json:"set_variables"`

	LogLevel  string `json:"log_level"`
	LogFormat string `json:"log_format"`
}

// runPackageDeploy deploys one package onto an initialised cluster.
func runPackageDeploy(ctx context.Context, run Runner, args *Args, resp *Response) error {
	var p deployParams
	if err := args.Params(&p); err != nil {
		return err
	}
	if p.Package == "" {
		return errors.New(`the "package" parameter is required: it is the package to deploy, ` +
			`as a path, an oci:// reference, or an https:// URL`)
	}

	if args.Control.CheckMode {
		// Same answer as init, for the same reason: zarf has no dry run to deploy with, and
		// deploying anyway during a run an operator asked to be told about is the one thing check
		// mode must not do.
		resp.Skipped = true
		resp.Msg = "check mode: zarf package deploy has no dry run, so nothing was done"
		resp.Zarf.ChangedSignal = SignalUnknown
		return nil
	}

	dir, err := deployWorkingDir(&p)
	if err != nil {
		return err
	}

	hb := NewHeartbeat(p.StatusFile)
	defer hb.Clear()
	resp.Zarf.StatusFile = hb.Path()

	prog := newProgress(hb, componentCount(p.Components))
	argv := buildDeployArgs(&p)

	binary := deployBinary(&p)
	resp.Zarf.Command = append([]string{binary}, redact(argv)...)
	resp.Zarf.Directory = dir

	res, runErr := run.Run(ctx, Invocation{
		Binary: binary,
		Args:   argv,
		Dir:    dir,
		Env:    deployEnv(&p, hb),
		OnLine: prog.line,
	})
	resp.Zarf.ExitCode = res.ExitCode
	resp.Zarf.ComponentsRan = prog.components

	if runErr != nil {
		prog.finish("failed", runErr)
		reportChanged(resp, prog)
		return fmt.Errorf("zarf package deploy failed: %w", runErr)
	}
	prog.finish("completed", nil)

	reportChanged(resp, prog)
	resp.Msg = "deployed " + p.Package
	return nil
}

// deployWorkingDir is the directory zarf runs in.
//
// Unlike init, a deploy names its package outright, so there is nothing to infer: an operator who
// said nothing gets the directory the module was invoked from, which is the one Ansible chose.
func deployWorkingDir(p *deployParams) (string, error) {
	if p.Directory == "" {
		return "", nil
	}
	info, err := os.Stat(p.Directory)
	if err != nil {
		return "", fmt.Errorf("unable to read the working directory %s: %w", p.Directory, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("the %q parameter is %s, which is not a directory", "directory", p.Directory)
	}
	return p.Directory, nil
}

func deployBinary(p *deployParams) string {
	if p.Binary != "" {
		return p.Binary
	}
	if env := os.Getenv(EnvBinary); env != "" {
		return env
	}
	return defaultBinary
}

func deployEnv(p *deployParams, hb *Heartbeat) []string {
	var env []string
	if p.Kubeconfig != "" {
		env = append(env, "KUBECONFIG="+p.Kubeconfig)
	}
	if p.ZarfConfig != "" {
		env = append(env, "ZARF_CONFIG="+p.ZarfConfig)
	}
	if hb.Path() != "" {
		// The wrapper writes the heartbeats, not zarf. See childEnv.
		env = append(env, EnvStatusFile+"=")
	}
	env = append(env, "NO_COLOR=1")
	return env
}

// buildDeployArgs renders the parameters as the zarf command line an operator would have typed.
func buildDeployArgs(p *deployParams) []string {
	c := newCommand("package", "deploy", deployPackage(p))

	c.bare(flagConfirm)
	c.bare(flagNoColor)

	format := p.LogFormat
	if format == "" {
		format = "json"
	}
	c.flag(flagLogFormat, format)
	c.flag(flagLogLevel, p.LogLevel)

	c.flag(flagComponents, p.Components)
	c.flag(flagNamespace, p.Namespace)
	c.flag(flagShasum, p.Shasum)

	c.boolFlag(flagConnected, p.Connected)
	c.boolFlag(flagForceConflicts, p.ForceConflicts)
	c.boolFlag(flagSkipValuesSchemaValidation, p.SkipValuesSchemaValid)
	c.boolFlag(flagInsecureSkipTLSVerify, p.InsecureSkipTLSVerify)
	c.boolFlag(flagPlainHTTP, p.PlainHTTP)
	c.boolFlag(flagTakeOwnership, p.TakeOwnership)

	c.intFlag(flagRetries, p.Retries)
	c.intFlag(flagOCIConcurrency, p.OCIConcurrency)
	c.flag(flagArchitecture, p.Architecture)
	c.flag(flagCache, p.Cache)
	c.flag(flagTmpdir, p.Tmpdir)
	c.flag(flagTimeout, p.Timeout)
	c.flag(flagKey, p.PublicKey)
	c.valueFlag(flagVerify, p.Verify)

	c.repeated(flagValues, p.Values)
	c.pairs(flagSetValues, p.SetValues)
	c.pairs(flagSetVariables, p.SetVariables)

	return c.args()
}

// deployPackage is the package as the command line should carry it.
//
// A local path is made absolute, because the working directory a deploy runs in is whatever
// Ansible chose unless the operator named one, and a relative path that resolved against the
// wrong directory is a failure that reads as a missing package. A remote reference is left alone:
// oci:// and https:// are not paths.
func deployPackage(p *deployParams) string {
	if isRemotePackage(p.Package) {
		return p.Package
	}
	if filepath.IsAbs(p.Package) {
		return p.Package
	}
	if p.Directory != "" {
		// The command runs in Directory, so a path relative to it is already right, and making it
		// absolute here would be the same string with more ways to be wrong.
		return p.Package
	}
	abs, err := filepath.Abs(p.Package)
	if err != nil {
		return p.Package
	}
	return abs
}

func isRemotePackage(pkg string) bool {
	return strings.HasPrefix(pkg, "oci://") ||
		strings.HasPrefix(pkg, "https://") ||
		strings.HasPrefix(pkg, "http://")
}
