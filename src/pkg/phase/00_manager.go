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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	v1alpha1 "github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/zarf-distro/src/types/distro"
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
	Prepare(*v1alpha1.ZarfCluster) error
}

type conditional interface {
	ShouldRun() bool
}

type withcleanup interface {
	CleanUp()
}

type withmanager interface {
	SetManager(*Manager)
}

type withDryRun interface {
	DryRun() error
}

// In-phase hooks for phases to run logic immediately before/after Run().
// These are strictly internal hooks for phases themselves and are separate
// from user-configured lifecycle hooks handled by the RunHooks phase.
type withBefore interface {
	Before() error
}
type withAfter interface {
	After() error
}

// Manager executes phases to construct the cluster
type Manager struct {
	phases            Phases
	Config            *v1alpha1.ZarfCluster
	Concurrency       int
	ConcurrentUploads int
	DryRun            bool
	Writer            io.Writer
	DistroCfg         ManagerDistroConfig
	dryMessages       map[string][]string
	dryMu             sync.Mutex
}

type ManagerDistroConfig struct {
	BinaryDir string
	Binary    string
	Config    string
	Token     string
	Data      string
}

// NewManager creates a new Manager
func NewManager(config *v1alpha1.ZarfCluster, distro distro.Distro) (*Manager, error) {
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

func (m *Manager) GetDistroBinaryName() string {
	if m.DistroCfg.Binary != "" {
		return m.DistroCfg.Binary
	}
	return ""
}

func (m *Manager) GetDistroBinaryDir() string {
	if m.DistroCfg.BinaryDir != "" {
		return m.DistroCfg.BinaryDir
	}
	return ""
}

func (m *Manager) GetDistroBinaryFull() string {
	if m.DistroCfg.BinaryDir != "" && m.DistroCfg.Binary != "" {
		return filepath.Join(m.DistroCfg.BinaryDir, m.DistroCfg.Binary)
	}
	return ""
}

type errorfunc func() error

// DryMsg prints a message in dry-run mode
func (m *Manager) DryMsg(host fmt.Stringer, msg string) {
	m.dryMu.Lock()
	defer m.dryMu.Unlock()
	if m.dryMessages == nil {
		m.dryMessages = make(map[string][]string)
	}
	var key string
	if host == nil {
		key = "local"
	} else {
		key = host.String()
	}
	m.dryMessages[key] = append(m.dryMessages[key], msg)
}

// Wet runs the first given function when not in dry-run mode. The second function will be
// run when in dry-mode and the message will be displayed. Any error returned from the
// functions will be returned and will halt the operation.
func (m *Manager) Wet(host fmt.Stringer, msg string, funcs ...errorfunc) error {
	if !m.DryRun {
		if len(funcs) > 0 && funcs[0] != nil {
			return funcs[0]()
		}
		return nil
	}

	m.DryMsg(host, msg)

	if m.DryRun && len(funcs) == 2 && funcs[1] != nil {
		return funcs[1]()
	}

	return nil
}

// Run executes all the added Phases in order
func (m *Manager) Run(ctx context.Context) error {
	var ran []Phase
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
					l.Info("* Running clean-up", "phase", p.Title())
					c.CleanUp()
				}
			}
		}
		if m.DryRun {
			if len(m.dryMessages) == 0 {
				l.Info("dry-run: no cluster state altering actions would be performed")
				return
			}

			l.Info("dry-run: cluster state altering actions would be performed:")
			for host, msgs := range m.dryMessages {
				l.Info("dry-run:", "host", host)
				for _, msg := range msgs {
					l.Info("dry-run:", "msg", msg)
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

		if p, ok := p.(withconfig); ok {
			l.Debug("Preparing", "phase", p.Title())
			if err := p.Prepare(m.Config); err != nil {
				result = err
				return result
			}
		}

		if p, ok := p.(conditional); ok {
			if !p.ShouldRun() {
				continue
			}
		}

		// Run in-phase before hook if implemented.
		if bp, ok := p.(withBefore); ok {
			l.Debug("running before", "phase", p.Title())
			if err := bp.Before(); err != nil {
				l.Debug("running before", "error", err.Error())
				result = err
				return result
			}
		}

		l.Info("Running", "phase", title)

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
				if herr := ap.After(); herr != nil {
					result = herr
					return result
				}
			}
		}

		if result != nil {
			return result
		}
	}

	return nil
}
