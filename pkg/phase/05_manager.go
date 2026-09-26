// Copyright 2023 k0sctl authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
//
// Modifications Copyright 2026 colonel-byte.
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
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/internal/heartbeat"
	"github.com/colonel-byte/cargoship/pkg/retry"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/creasty/defaults"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// NoWait is used by various phases to decide if node ready state should be waited for or not
var NoWait bool

// Force is used by various phases to attempt a forced installation
var Force bool

// Phase represents a runnable phase which can be added to Manager.
type Phase interface {
	Run(context.Context) error
	Title() string
	Explanation() string
}

// Phases is a slice of Phases
type Phases []Phase

// Index returns the index of the first occurrence matching the given phase title or -1 if not found
func (p Phases) Index(title string) int {
	for i, phase := range p {
		if phase.Title() == title {
			return i
		}
	}
	return -1
}

// InsertAfter inserts a phase after the first occurrence of a phase with the given title
func (p *Phases) InsertAfter(title string, phase Phase) {
	i := p.Index(title)
	if i == -1 {
		return
	}
	*p = append((*p)[:i+1], append(Phases{phase}, (*p)[i+1:]...)...)
}

// InsertBefore inserts a phase before the first occurrence of a phase with the given title
func (p *Phases) InsertBefore(title string, phase Phase) {
	i := p.Index(title)
	if i == -1 {
		return
	}
	*p = append((*p)[:i], append(Phases{phase}, (*p)[i:]...)...)
}

// Replace replaces the first occurrence of a phase with the given title
func (p *Phases) Replace(title string, phase Phase) {
	i := p.Index(title)
	if i == -1 {
		return
	}
	(*p)[i] = phase
}

type withconfig interface {
	Title() string
	Prepare(context.Context, *cluster.ZarfCluster, *distro.ZarfDistro) error
}

type conditional interface {
	ShouldRun() bool
}

type withcleanup interface {
	CleanUp(context.Context)
}

type withmanager interface {
	SetManager(*Manager)
}

type withDryRun interface {
	DryRun() error
}

// readOnly marks a phase that reads from hosts but never changes them, so a dry run can run it
// as itself rather than reporting it.
//
// A phase that implements neither this nor withDryRun is skipped under a dry run. That default
// is the point of the interface: dry-run safety is a property a phase has to state, so a phase
// added later is safe without anyone having remembered to think about it.
//
// ReadOnly returns why the phase is safe to run and why a dry run wants it run. The reason is a
// return value rather than a comment because magefiles/gen-docs.go renders it into
// docs/phases/<name>.md next to the phase, so the claim a reader sees is the one the phase
// makes, not a second copy of it that can drift.
type readOnly interface {
	ReadOnly() string
}

// DryRunBehavior is what a dry run does with a phase. It is derived from the interfaces the
// phase implements, and it is exported so magefiles/gen-docs.go can label each phase in
// docs/phases/<name>.md with the same classification Run() gates on. The docs and the gate
// cannot disagree, because there is only one classifier.
type DryRunBehavior int

const (
	// DryRunSkip is the default: the phase declares nothing, so a dry run reports it and
	// does not run it.
	DryRunSkip DryRunBehavior = iota
	// DryRunReadOnly is a phase implementing readOnly. It reads hosts and changes nothing,
	// so a dry run runs it as itself.
	DryRunReadOnly
	// DryRunOwnPath is a phase implementing withDryRun. A dry run calls DryRun() instead of
	// Run().
	DryRunOwnPath
)

// String is the label written into the phase docs.
func (b DryRunBehavior) String() string {
	switch b {
	case DryRunReadOnly:
		return "runs, reads only"
	case DryRunOwnPath:
		return "runs its own dry-run path"
	default:
		return "reported, not run"
	}
}

// ClassifyDryRun reports what a dry run does with p.
func ClassifyDryRun(p Phase) DryRunBehavior {
	if _, ok := p.(withDryRun); ok {
		return DryRunOwnPath
	}
	if _, ok := p.(readOnly); ok {
		return DryRunReadOnly
	}
	return DryRunSkip
}

// DryRunNote returns the phase's own account of why a dry run runs it, or "" for a phase that
// gives none. Only read-only phases carry one: they are the phases a dry run runs against live
// hosts, so they are the ones a reader is entitled to an argument about.
func DryRunNote(p Phase) string {
	if ro, ok := p.(readOnly); ok {
		return ro.ReadOnly()
	}
	return ""
}

// changedReporter is a phase that knows whether it changed anything on the fleet.
//
// It sits beside readOnly and withDryRun as a third thing a phase may declare about itself, and
// like them it is opt-in. A phase that does not implement it is not taken to have changed
// nothing: it is recorded as having said nothing, and RunResult reports the answer as incomplete
// so a caller is told the signal is partial rather than handed a confident wrong one.
//
// Changed is read after the run rather than before it, so a phase answers from what it did and
// not from what it expected to do. A phase that failed halfway through still changed the fleet,
// and is expected to say so.
//
// The caller this exists for is Ansible's changed, in internal/ansiblemod.
type changedReporter interface {
	Changed() bool
}

// PhaseResult is what a single phase did during a run.
type PhaseResult struct {
	Title       string
	Explanation string
	// DryRun is what a dry run does with this phase, whether or not this run was one.
	DryRun DryRunBehavior
	// Declared is whether the phase implements changedReporter. When it is false, Changed is
	// false because nothing was asked, not because nothing happened.
	Declared bool
	Changed  bool
}

// RunResult is what a whole Manager.Run did, phase by phase. Manager.Run fills it in on the way
// through, so a run that returned an error still carries the phases it got through first.
type RunResult struct {
	// Ran are the phases that executed, in order. That includes the read-only phases a dry run
	// runs as themselves and the phases that took their own dry-run path. Running is not
	// changing: Connect, DetectOS, GatherFacts and ValidateHosts all land here and change
	// nothing, which is why Changed is asked separately.
	Ran []PhaseResult
	// Planned are the phases a dry run reported instead of running.
	Planned []PhaseResult
	// Skipped are the phases ShouldRun filtered out. They are kept because a phase can finish
	// its work in Prepare and then have nothing left to run: EngineConfigSyncHosts writes the
	// files the engine re-reads without a restart while it is deciding which hosts have
	// drifted, and then reports no hosts to sync.
	Skipped []PhaseResult
}

// Changed reports whether any phase said it changed something on the fleet. Phases that declared
// nothing do not contribute; read Complete to find out whether that leaves the answer partial.
func (r RunResult) Changed() bool {
	for _, group := range [][]PhaseResult{r.Ran, r.Skipped} {
		for _, p := range group {
			if p.Declared && p.Changed {
				return true
			}
		}
	}
	return false
}

// Outstanding is the dry-run counterpart of Changed: it reports whether any phase that can
// report a change was reported rather than run. A phase reaches Planned only after its ShouldRun
// said there was work to do, so this answers "would a real run change anything" for the phases
// that have said how to tell.
func (r RunResult) Outstanding() bool {
	for _, p := range r.Planned {
		if p.Declared {
			return true
		}
	}
	return false
}

// Undeclared names the phases that did not say whether they changed anything, in the order the
// run reached them. A caller reporting Changed is expected to report these alongside it.
func (r RunResult) Undeclared() []string {
	var out []string
	for _, group := range [][]PhaseResult{r.Ran, r.Planned, r.Skipped} {
		for _, p := range group {
			if !p.Declared {
				out = append(out, p.Title)
			}
		}
	}
	return out
}

// Complete reports whether every phase the run reached declared whether it changed anything.
func (r RunResult) Complete() bool {
	return len(r.Undeclared()) == 0
}

// Titles names the phases in a slice, for a caller that reports a list of names rather than the
// detail behind them.
func Titles(results []PhaseResult) []string {
	if len(results) == 0 {
		return nil
	}
	out := make([]string, 0, len(results))
	for _, p := range results {
		out = append(out, p.Title)
	}
	return out
}

// merge appends every phase of other onto r, keeping order.
func (r RunResult) merge(other RunResult) RunResult {
	r.Ran = append(r.Ran, other.Ran...)
	r.Planned = append(r.Planned, other.Planned...)
	r.Skipped = append(r.Skipped, other.Skipped...)
	return r
}

type resultSinkKey struct{}

// ResultSink collects the RunResult of every Manager.Run that happens on a context.
//
// It exists because the Ansible module mode runs cargoship by building an argument vector and
// calling the ordinary command, which returns an error and nothing else -- see
// src/cmd.ExecuteArgs. Threading a return value back through cobra, the action and the manager
// would change five call sites to serve one caller. A sink on the context that nobody else
// installs does not, and a run with no sink on its context pays nothing for this.
type ResultSink struct {
	mu       sync.Mutex
	observed bool
	result   RunResult
}

// WithResultSink returns a context carrying a sink, and the sink itself.
func WithResultSink(ctx context.Context) (context.Context, *ResultSink) {
	s := &ResultSink{}
	return context.WithValue(ctx, resultSinkKey{}, s), s
}

func (s *ResultSink) publish(r RunResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observed = true
	s.result = s.result.merge(r)
}

// Result is every run that reached its phases on this context, flattened into one.
func (s *ResultSink) Result() RunResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result
}

// Observed reports whether any run reached its phases at all. A command that failed before that
// -- an unreadable package, a configuration that does not validate -- publishes nothing, and
// that is a different thing from a run in which no phase changed anything.
func (s *ResultSink) Observed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.observed
}

// phaseResult records what one phase did, asking it whether it changed anything if it is willing
// to say.
func phaseResult(p Phase) PhaseResult {
	r := PhaseResult{
		Title:       p.Title(),
		Explanation: p.Explanation(),
		DryRun:      ClassifyDryRun(p),
	}
	if c, ok := p.(changedReporter); ok {
		r.Declared = true
		r.Changed = c.Changed()
	}
	return r
}

// collectResult turns the phase lists Run kept into the result it publishes.
func collectResult(ran, planned, skipped []Phase) RunResult {
	var out RunResult
	for _, p := range ran {
		out.Ran = append(out.Ran, phaseResult(p))
	}
	for _, p := range planned {
		out.Planned = append(out.Planned, phaseResult(p))
	}
	for _, p := range skipped {
		out.Skipped = append(out.Skipped, phaseResult(p))
	}
	return out
}

// In-phase hooks for phases to run logic immediately before/after Run().
// These are strictly internal hooks for phases themselves and are separate
// from user-configured lifecycle hooks handled by the RunHooks phase.
type withBefore interface {
	Before(context.Context) error
}
type withAfter interface {
	After(context.Context) error
}

// Manager executes phases to construct the cluster
type Manager struct {
	phases Phases
	Config *cluster.ZarfCluster
	Distro *distro.ZarfDistro
	// Values is the configuration the package ships with, merged with the
	// overrides in the cluster inventory and checked against the package's
	// values schema. Phases read it instead of re-reading either source, so
	// every phase sees the same values.
	Values            map[string]any
	DistroID          string
	Concurrency       int
	ConcurrentUploads int
	DryRun            bool
	Writer            io.Writer
	TempDirectory     string
	Timeout           time.Duration
	// Result is what the last Run did, phase by phase. Run fills it in whether it succeeded or
	// failed, so a caller reading it after an error sees the phases the run got through. It is
	// a field rather than a return value because Run has five call sites and one caller that
	// wants this.
	Result RunResult
}

// ManagerDistroConfig stores some values for manager distro config
type ManagerDistroConfig struct {
	BinaryDir string
	Binary    string
	Config    string
	Token     string
	Data      string
	Version   string
}

// NewManager creates a new Manager
func NewManager(config *cluster.ZarfCluster, distro distrocfg.Distro) (*Manager, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if distro == nil {
		return nil, fmt.Errorf("distro is nil")
	}

	return &Manager{Config: config, Writer: os.Stdout}, nil
}

// AddPhase adds a Phase to Manager
func (m *Manager) AddPhase(p ...Phase) {
	m.phases = append(m.phases, p...)
}

// SetPhases sets the list of phases
func (m *Manager) SetPhases(p Phases) {
	m.phases = p
}

// SetTimout sets the timeout for the manager
func (m *Manager) SetTimout(tm time.Duration) {
	m.Timeout = tm
}

// RetryTimeout wraps retry Timeout logic
func (m *Manager) RetryTimeout(ctx context.Context, f func(ctx context.Context) error) error {
	if m.Timeout > 0 {
		return retry.Timeout(ctx, m.Timeout, f)
	}
	return retry.WithDefaultTimeout(ctx, f)
}

// GetDistroOSFiles returns the ZarfFiles for a distro
func (m *Manager) GetDistroOSFiles() v1alpha1.ZarfFiles {
	return m.Distro.Spec.Config.OS.Files
}

type errorfunc func() error

// Wet runs the first given function when not in dry-run mode. The second function will be
// run when in dry-mode and the message will be displayed. Any error returned from the
// functions will be returned and will halt the operation.
func (m *Manager) Wet(_ fmt.Stringer, _ string, funcs ...errorfunc) error {
	if !m.DryRun {
		if len(funcs) > 0 && funcs[0] != nil {
			return funcs[0]()
		}
		return nil
	}

	if m.DryRun && len(funcs) == 2 && funcs[1] != nil {
		return funcs[1]()
	}

	return nil
}

// Run executes all the added Phases in order
func (m *Manager) Run(ctx context.Context) error {
	var ran []Phase
	// planned collects the phases a dry run reported instead of running, for the summary below.
	var planned []Phase
	// skipped collects the phases ShouldRun filtered out. They run nothing, but a phase can
	// have finished its work in Prepare, so they are recorded for the changed signal.
	var skipped []Phase
	var result error

	l := logger.From(ctx)

	if m.Config == nil {
		return fmt.Errorf("cannot run phases: config is nil")
	}

	l.Debug("setting defaults")
	if err := defaults.Set(m.Config); err != nil {
		return fmt.Errorf("failed to set defaults: %w", err)
	}

	defer func() {
		if result != nil {
			for _, p := range ran {
				if c, ok := p.(withcleanup); ok {
					l.Info("running clean-up", "phase", p.Title())
					c.CleanUp(ctx)
				}
			}
		}
	}()

	// Registered after the clean-up defer so it runs before it: what a phase changed is what it
	// changed, and a caller reading the result should not be shown the state clean-up left
	// behind. Registered after the two returns above so a run that never reached its phases
	// publishes nothing, which a sink reports as a missing answer rather than as no change.
	defer func() {
		m.Result = collectResult(ran, planned, skipped)
		if sink, ok := ctx.Value(resultSinkKey{}).(*ResultSink); ok {
			sink.publish(m.Result)
		}
	}()

	totalPhases := len(m.phases)
	for i, p := range m.phases {
		title := p.Title()
		heartbeat.Update(title, i+1, totalPhases, "running", nil)

		if err := ctx.Err(); err != nil {
			result = fmt.Errorf("context canceled before entering phase %q: %w", title, err)
			heartbeat.Update(title, i+1, totalPhases, "failed", result)
			return result
		}

		if p, ok := p.(withmanager); ok {
			p.SetManager(m)
		}

		// Classify before Prepare, so that when Prepare fails a dry run can tell a preflight
		// result from an artifact of the phases it just skipped. See the two uses below.
		skipUnderDryRun := m.DryRun && ClassifyDryRun(p) == DryRunSkip

		if cp, ok := p.(withconfig); ok {
			l.Debug("preparing", "phase", title)
			if err := cp.Prepare(ctx, m.Config, m.Distro); err != nil {
				// A phase a dry run was going to skip cannot always prepare, because its
				// Prepare reads state an earlier skipped phase would have created: KubeConfig
				// and LabelNodes both look for a running controller and return
				// ErrNoControllers when there is none. Failing here would make --dry-run
				// useless against the cluster it is worth most on, the one with nothing
				// installed yet.
				//
				// So report the phase and say the assessment is partial. The cost is the
				// ShouldRun filter below, which this skips for that one phase, so it may be
				// listed when a real run would not have reached it. That is the safe direction
				// to be wrong in: a dry run that names one phase too many is read and
				// discounted, one that names too few is believed.
				if !skipUnderDryRun {
					result = err
					return result
				}
				l.Info("would run", "phase", title, "unassessed", err.Error())
				planned = append(planned, p)
				continue
			}
		}

		if c, ok := p.(conditional); ok {
			if !c.ShouldRun() {
				skipped = append(skipped, p)
				continue
			}
		}

		// A dry run only gets past here for a phase that has said how it behaves under one:
		// readOnly, meaning it reads hosts and changes nothing, or withDryRun, meaning it has
		// its own dry path. Everything else is reported and skipped. That default is deliberate
		// -- a phase added later is safe without anyone having remembered to think about it.
		//
		// The skip is above the before hook because a hook that fires for a phase we are about
		// to skip changes the host by another route.
		//
		// Lock lands here, which is the whole reason it is not special-cased: taking the cluster
		// lock is not something to do a half version of. So a dry run holds no lock. It cannot
		// block a real apply, and it can report state a concurrent apply is already moving. The
		// matching Unlock is skipped with it, so nothing releases a lock that was never taken.
		if skipUnderDryRun {
			l.Info("would run", "phase", title)
			planned = append(planned, p)
			continue
		}

		// Run in-phase before hook if implemented.
		if bp, ok := p.(withBefore); ok {
			l.Debug("running before", "phase", p.Title())
			if err := bp.Before(ctx); err != nil {
				l.Debug("running before", "error", err.Error())
				result = err
				return result
			}
		}

		l.Info("running", "phase", title)

		if dp, ok := p.(withDryRun); ok && m.DryRun {
			ran = append(ran, p)
			if err := dp.DryRun(); err != nil {
				result = err
				return result
			}
			continue
		}

		result = p.Run(ctx)
		ran = append(ran, p)

		// Only run in-phase After hook if Run() succeeded.
		// If After() fails after a successful Run(), return the After() error.
		if result == nil {
			if ap, ok := p.(withAfter); ok {
				l.Debug("running after", "phase", p.Title())
				if herr := ap.After(ctx); herr != nil {
					result = herr
					return result
				}
			}
		}

		if result != nil {
			heartbeat.Update(title, i+1, totalPhases, "failed", result)
			return result
		}
		heartbeat.Update(title, i+1, totalPhases, "completed", nil)
	}

	if m.DryRun {
		m.logDryRunSummary(ctx, ran, planned)
	}

	heartbeat.Clear()
	return nil
}

// logDryRunSummary reports what a dry run did and what it left alone.
//
// Each planned phase is listed with the same Explanation() that magefiles/gen-docs.go renders
// into docs/phases/<name>.md, so the run and the docs describe a phase in the same words by
// construction rather than by anyone keeping two strings in step.
//
// The list is not a static roster. Every phase's Prepare and ShouldRun has already run against
// the live hosts, and both only read, so a phase whose work is already done -- an engine that is
// running, a file that is present -- is filtered out before it reaches here.
func (m *Manager) logDryRunSummary(ctx context.Context, ran, planned []Phase) {
	l := logger.From(ctx)

	l.Info("dry run finished", "ran", len(ran), "wouldRun", len(planned))
	for _, p := range planned {
		l.Info("would run", "phase", p.Title(), "detail", p.Explanation())
	}
}
