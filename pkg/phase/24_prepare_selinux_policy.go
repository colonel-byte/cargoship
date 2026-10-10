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
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	// SELinuxPolicyDir is the directory on the host holding cargoship's CIL policy modules
	SELinuxPolicyDir = "/var/lib/cargoship/selinux"
	// SELinuxStateFile records the settings cargoship changed, so a reset can restore them
	SELinuxStateFile = SELinuxPolicyDir + "/cargoship-state.json"
	// SEModule name of the binary that installs a policy module
	SEModule = "semodule"
	// DefaultSELinuxModulePriority is the semodule priority a module installs at when it sets
	// none. It sits above the distribution policy's 100 so a package module overrides rather
	// than conflicts with the shipped policy.
	DefaultSELinuxModulePriority = 400
	// DefaultSELinuxFileType is the semanage fcontext file type used when a mapping sets none
	DefaultSELinuxFileType = "all"
)

// selinuxState is the on-host record of what cargoship applied, and the only input the removal
// phase has: a reset loads no distro package, so it cannot read spec.config.os.selinux and has to
// learn what to unwind from the host itself. Booleans are recorded with the value they held
// beforehand, since a boolean that has been set has no discoverable default to return to.
type selinuxState struct {
	// Booleans maps a boolean name to the value it held before cargoship first changed it.
	Booleans map[string]bool `json:"booleans,omitempty"`
	// Modules lists the policy modules cargoship installed.
	Modules []selinuxStateModule `json:"modules,omitempty"`
	// FileContexts lists the file context mappings cargoship added.
	FileContexts []selinuxStateFileContext `json:"fileContexts,omitempty"`
}

// selinuxStateModule is one installed module, carrying the priority needed to remove it again.
type selinuxStateModule struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
}

// selinuxStateFileContext is one added mapping, carrying what semanage fcontext -d needs.
type selinuxStateFileContext struct {
	Path     string `json:"path"`
	Type     string `json:"type"`
	FileType string `json:"fileType"`
}

// PrepareSelinuxPolicy installs the distro-supplied SELinux policy on the hosts.
type PrepareSelinuxPolicy struct {
	GenericPhase
	selinuxPolicyHosts cluster.ZarfHosts
}

// Prepare the phase
func (p *PrepareSelinuxPolicy) Prepare(ctx context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	p.selinuxPolicyHosts = p.manager.Config.Spec.Hosts.Filter(selinuxPolicyCapable)

	logger.From(ctx).Info("number of systems that can take an selinux policy", "hosts", len(p.selinuxPolicyHosts))

	return nil
}

// Title for the phase
func (p *PrepareSelinuxPolicy) Title() string {
	return "Prepare hosts - Enterprise Linux support - SELinux policy"
}

// Explanation about the current phase, used for documentation generation
func (p *PrepareSelinuxPolicy) Explanation() string {
	return "Installs the policy modules, booleans and file contexts from spec.config.os.selinux on hosts running SELinux in enforcing mode, using semodule, setsebool and semanage fcontext"
}

// Run the phase
func (p *PrepareSelinuxPolicy) Run(ctx context.Context) error {
	return p.parallelDoWithMessage(
		ctx,
		"applying selinux policy",
		p.selinuxPolicyHosts,
		p.recordState,
		p.applyModules,
		p.applyBooleans,
		p.applyFileContexts,
	)
}

// ShouldRun is true when a host can take a policy and the distro package supplies one
func (p *PrepareSelinuxPolicy) ShouldRun() bool {
	return len(p.selinuxPolicyHosts) > 0 && !p.manager.Distro.Spec.Config.OS.SELinux.IsZero()
}

// applyModules writes every CIL module to the host and installs it. The modules run ahead of the
// booleans and file contexts because a module can define the type a context refers to.
func (p *PrepareSelinuxPolicy) applyModules(ctx context.Context, h *cluster.ZarfHost) error {
	modules := p.manager.Distro.Spec.Config.OS.SELinux.Modules
	if len(modules) == 0 {
		return nil
	}

	installed, err := installedSELinuxModules(h)
	if err != nil {
		return err
	}

	for _, m := range modules {
		path := selinuxModulePath(m.Name)

		current := ""
		if h.FileExist(path) {
			current, err = h.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", path, err)
			}
		}

		// Both halves matter. Matching content with the module absent means an earlier run wrote
		// the file and then failed to install it, and skipping would leave the policy inactive.
		if current == m.CIL && installed[m.Name] {
			logger.From(ctx).Debug("selinux module already installed", "host", h, "module", m.Name)
			continue
		}

		if current != m.CIL {
			if err := p.Wet(h, "write "+path, func() error {
				return h.WriteFile(path, m.CIL, "0600")
			}); err != nil {
				return fmt.Errorf("failed to write %s: %w", path, err)
			}
		}

		install := fmt.Sprintf("%s -X %d -i %s", SEModule, selinuxModulePriority(m), shellQuote(path))
		logger.From(ctx).Info("installing selinux module", "host", h, "module", m.Name, "priority", selinuxModulePriority(m))
		if err := p.Wet(h, install, func() error {
			return h.SudoExec(install)
		}); err != nil {
			return fmt.Errorf("failed to install selinux module %q: %w", m.Name, err)
		}
	}

	return nil
}

// applyBooleans sets every configured boolean persistently, recording the values the host held
// beforehand so RemoveSelinuxPolicy can put them back.
func (p *PrepareSelinuxPolicy) applyBooleans(ctx context.Context, h *cluster.ZarfHost) error {
	booleans := p.manager.Distro.Spec.Config.OS.SELinux.Booleans
	if len(booleans) == 0 {
		return nil
	}

	names := slices.Sorted(maps.Keys(booleans))

	set := "setsebool -P " + strings.Join(selinuxBooleanArgs(booleans, names), " ")
	logger.From(ctx).Info("setting selinux booleans", "host", h, "booleans", names)
	if err := p.Wet(h, set, func() error {
		return h.SudoExec(set)
	}); err != nil {
		return fmt.Errorf("failed to set selinux booleans: %w", err)
	}

	return nil
}

// applyFileContexts adds every configured file context mapping and relabels the paths it covers.
func (p *PrepareSelinuxPolicy) applyFileContexts(ctx context.Context, h *cluster.ZarfHost) error {
	contexts := p.manager.Distro.Spec.Config.OS.SELinux.FileContexts
	if len(contexts) == 0 {
		return nil
	}

	for _, fc := range contexts {
		add := selinuxFcontextCommand("-a", fc)
		logger.From(ctx).Info("adding selinux file context", "host", h, "path", fc.Path, "type", fc.Type)
		if err := p.Wet(h, add, func() error {
			if err := h.SudoExec(add); err == nil {
				return nil
			}
			// semanage rejects a mapping it already holds, so -a is not re-runnable. Modifying
			// the existing mapping is what makes a second apply land on the configured type.
			modify := selinuxFcontextCommand("-m", fc)
			logger.From(ctx).Debug("file context exists, modifying it instead", "host", h, "path", fc.Path)
			return h.SudoExec(modify)
		}); err != nil {
			return fmt.Errorf("failed to add selinux file context for %q: %w", fc.Path, err)
		}

		if err := p.relabelSELinux(ctx, h, fc.Path); err != nil {
			return err
		}
	}

	return nil
}

// relabel runs restorecon over the paths a mapping covers. restorecon takes a real path and the
// mapping holds a regular expression, so the literal prefix is resolved against the host rather
// than guessed: the prefix itself when it exists, otherwise its parent directory.
func (p *GenericPhase) relabelSELinux(ctx context.Context, h *cluster.ZarfHost, path string) error {
	target := selinuxRelabelTarget(h, path)
	if target == "" {
		logger.From(ctx).Warn("no existing path to relabel for selinux file context", "host", h, "path", path)
		return nil
	}

	restore := "restorecon -R -F " + shellQuote(target)
	if err := p.Wet(h, restore, func() error {
		return h.SudoExec(restore)
	}); err != nil {
		return fmt.Errorf("failed to relabel %q: %w", target, err)
	}

	return nil
}

// recordState writes what this apply is about to do to the host's state file, before anything is
// changed. Booleans already recorded keep the value written the first time, which is the one the
// host had before cargoship touched it; the module and file context lists are replaced, so they
// describe what is actually installed now.
func (p *PrepareSelinuxPolicy) recordState(ctx context.Context, h *cluster.ZarfHost) error {
	cfg := p.manager.Distro.Spec.Config.OS.SELinux

	state := selinuxState{Booleans: map[string]bool{}}
	if h.FileExist(SELinuxStateFile) {
		raw, err := h.ReadFile(SELinuxStateFile)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", SELinuxStateFile, err)
		}
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			logger.From(ctx).Warn("selinux state file is unreadable, rewriting it", "host", h, "file", SELinuxStateFile, "error", err)
			state = selinuxState{}
		}
		if state.Booleans == nil {
			state.Booleans = map[string]bool{}
		}
	}

	if len(cfg.Booleans) > 0 {
		names := slices.Sorted(maps.Keys(cfg.Booleans))
		prior, err := currentSELinuxBooleans(h, names)
		if err != nil {
			return err
		}
		for _, name := range names {
			if _, ok := state.Booleans[name]; ok {
				continue
			}
			state.Booleans[name] = prior[name]
		}
	}

	state.Modules = make([]selinuxStateModule, 0, len(cfg.Modules))
	for _, m := range cfg.Modules {
		state.Modules = append(state.Modules, selinuxStateModule{
			Name:     m.Name,
			Priority: selinuxModulePriority(m),
		})
	}

	state.FileContexts = make([]selinuxStateFileContext, 0, len(cfg.FileContexts))
	for _, fc := range cfg.FileContexts {
		state.FileContexts = append(state.FileContexts, selinuxStateFileContext{
			Path:     fc.Path,
			Type:     fc.Type,
			FileType: selinuxFileType(fc),
		})
	}

	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode selinux state: %w", err)
	}

	if err := p.Wet(h, "create "+SELinuxPolicyDir, func() error {
		return h.Sudo().FS().MkdirAll(SELinuxPolicyDir, 0o700)
	}); err != nil {
		return fmt.Errorf("failed to create %s: %w", SELinuxPolicyDir, err)
	}

	if err := p.Wet(h, "write "+SELinuxStateFile, func() error {
		return h.WriteFile(SELinuxStateFile, string(b)+"\n", "0600")
	}); err != nil {
		return fmt.Errorf("failed to write %s: %w", SELinuxStateFile, err)
	}

	return nil
}

// selinuxPolicyCapable reports whether h can take a policy module at all: SELinux has to be
// enforcing, and the tooling that installs a module has to be present.
func selinuxPolicyCapable(h *cluster.ZarfHost) bool {
	return selinuxEnabled(h) && h.FS().CommandExist(SEModule)
}

// installedSELinuxModules reads the module names semodule already knows about. Its output lists
// one module per line as "<priority> <name> <language>".
func installedSELinuxModules(h *cluster.ZarfHost) (map[string]bool, error) {
	out, err := h.SudoExecOutput(SEModule + " -lfull")
	if err != nil {
		return nil, fmt.Errorf("failed to list selinux modules: %w", err)
	}

	installed := map[string]bool{}
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		installed[fields[1]] = true
	}

	return installed, nil
}

// currentSELinuxBooleans reads the present value of every named boolean. getsebool writes one
// line per boolean as "<name> --> on".
func currentSELinuxBooleans(h *cluster.ZarfHost, names []string) (map[string]bool, error) {
	out, err := h.SudoExecOutput("getsebool " + strings.Join(names, " "))
	if err != nil {
		return nil, fmt.Errorf("failed to read selinux booleans: %w", err)
	}

	values := map[string]bool{}
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		values[fields[0]] = fields[2] == "on"
	}

	for _, name := range names {
		if _, ok := values[name]; !ok {
			return nil, fmt.Errorf("selinux boolean %q is not defined on the host", name)
		}
	}

	return values, nil
}

// selinuxBooleanArgs renders the key=value arguments for setsebool, in the order of names.
func selinuxBooleanArgs(booleans map[string]bool, names []string) []string {
	args := make([]string, 0, len(names))
	for _, name := range names {
		value := "off"
		if booleans[name] {
			value = "on"
		}
		args = append(args, name+"="+value)
	}
	return args
}

// selinuxFcontextCommand renders a semanage fcontext call for op, which is -a, -m or -d.
func selinuxFcontextCommand(op string, fc distro.ZarfDistroSELinuxFileContext) string {
	return fmt.Sprintf(
		"semanage fcontext %s -t %s -f %s %s",
		op,
		shellQuote(fc.Type),
		shellQuote(selinuxFileType(fc)),
		shellQuote(fc.Path),
	)
}

// selinuxModulePath is where the CIL source for name lives on the host.
func selinuxModulePath(name string) string {
	return filepath.Join(SELinuxPolicyDir, name+".cil")
}

// selinuxModulePriority is the priority m installs at, defaulted when it sets none.
func selinuxModulePriority(m distro.ZarfDistroSELinuxModule) int {
	if m.Priority <= 0 {
		return DefaultSELinuxModulePriority
	}
	return m.Priority
}

// selinuxFileType is the semanage file type fc applies to, defaulted when it sets none.
func selinuxFileType(fc distro.ZarfDistroSELinuxFileContext) string {
	if fc.FileType == "" {
		return DefaultSELinuxFileType
	}
	return fc.FileType
}

// selinuxRelabelTarget resolves the path regular expression to a directory on h that restorecon
// can be pointed at, or "" when there is none. "/" is never returned: relabeling the whole
// filesystem is never what a single mapping asked for.
func selinuxRelabelTarget(h *cluster.ZarfHost, path string) string {
	prefix := selinuxLiteralPrefix(path)
	if prefix == "" {
		return ""
	}
	if h.FileExist(prefix) {
		return prefix
	}

	// The prefix can end mid-segment, as in /var/lib/ran*her, so the parent is the next candidate.
	parent := filepath.Dir(prefix)
	if parent == "/" || parent == "." || !h.FileExist(parent) {
		return ""
	}

	return parent
}

// selinuxLiteralPrefix returns the leading part of a file context path that holds no regular
// expression metacharacters, with any trailing separator removed. It returns "" when that leaves
// nothing usable. "." is treated as literal: it is a metacharacter, but it appears far more often
// as part of a real filename.
func selinuxLiteralPrefix(path string) string {
	cut := strings.IndexAny(path, `([*?+{|\^$`)
	if cut >= 0 {
		path = path[:cut]
	}

	// A trailing "/." is what a cut at the "*" in /opt/cni/bin/.* leaves behind, so the dot goes
	// with the separator here even though "." is otherwise treated as a literal character.
	path = strings.TrimRight(path, "/.")
	if path == "" || !strings.HasPrefix(path, "/") {
		return ""
	}

	return path
}

// shellQuote wraps s for a POSIX shell, so a path holding a space or a metacharacter reaches the
// command as one argument.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
