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
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/pkg/retry"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
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
type readOnly interface {
	ReadOnly()
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
	phases            Phases
	Config            *cluster.ZarfCluster
	Distro            *distro.ZarfDistro
	DistroID          string
	Concurrency       int
	ConcurrentUploads int
	DryRun            bool
	Writer            io.Writer
	TempDirectory     string
	Timeout           time.Duration
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

	for _, p := range m.phases {
		title := p.Title()

		if err := ctx.Err(); err != nil {
			result = fmt.Errorf("context canceled before entering phase %q: %w", title, err)
			return result
		}

		if p, ok := p.(withmanager); ok {
			p.SetManager(m)
		}

		// Classify before Prepare, so that when Prepare fails a dry run can tell a preflight
		// result from an artifact of the phases it just skipped. See the two uses below.
		skipUnderDryRun := false
		if m.DryRun {
			_, hasDryRun := p.(withDryRun)
			_, isReadOnly := p.(readOnly)
			skipUnderDryRun = !hasDryRun && !isReadOnly
		}

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

		if p, ok := p.(conditional); ok {
			if !p.ShouldRun() {
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
			return result
		}
	}

	if m.DryRun {
		m.logDryRunSummary(ctx, ran, planned)
	}

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
