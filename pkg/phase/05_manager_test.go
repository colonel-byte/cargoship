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
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/stretchr/testify/require"
)

type conditionalPhase struct {
	shouldrunCalled bool
	runCalled       bool
}

func (p *conditionalPhase) Title() string {
	return "conditional phase"
}

func (p *conditionalPhase) Explanation() string {
	return "test function"
}

func (p *conditionalPhase) ShouldRun() bool {
	p.shouldrunCalled = true
	return false
}

func (p *conditionalPhase) Run(_ context.Context) error {
	p.runCalled = true
	return nil
}

func TestConditionalPhase(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}}
	p := &conditionalPhase{}
	m.SetPhases(Phases{p})
	require.NoError(t, m.Run(context.Background()))
	require.False(t, p.runCalled, "run was not called")
	require.True(t, p.shouldrunCalled, "shouldrun was not called")
}

type configPhase struct {
	receivedConfig bool
}

func (p *configPhase) Title() string {
	return "config phase"
}

func (p *configPhase) Explanation() string {
	return "test function"
}

func (p *configPhase) Prepare(_ context.Context, c *cluster.Cluster, _ *distro.Distro) error {
	p.receivedConfig = c != nil
	return nil
}

func (p *configPhase) Run(_ context.Context) error {
	return nil
}

func TestConfigPhase(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}}
	p := &configPhase{}
	m.SetPhases(Phases{p})
	require.NoError(t, m.Run(context.Background()))
	require.True(t, p.receivedConfig, "config was not received")
}

type hookedPhase struct {
	fn            func() error
	beforeCalled  bool
	afterCalled   bool
	cleanupCalled bool
	runCalled     bool
}

func (p *hookedPhase) Title() string {
	return "hooked phase"
}

func (p *hookedPhase) Explanation() string {
	return "test function"
}

func (p *hookedPhase) Before(_ context.Context) error {
	p.beforeCalled = true
	return nil
}

func (p *hookedPhase) After(_ context.Context) error {
	p.afterCalled = true
	return nil
}

func (p *hookedPhase) CleanUp(_ context.Context) {
	p.cleanupCalled = true
}

func (p *hookedPhase) Run(_ context.Context) error {
	p.runCalled = true
	if p.fn != nil {
		return p.fn()
	}
	return fmt.Errorf("run failed")
}

func TestHookedPhase(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}}
	p := &hookedPhase{}
	m.SetPhases(Phases{p})
	require.Error(t, m.Run(context.Background()))
	require.True(t, p.beforeCalled, "before hook was not called")
	require.False(t, p.afterCalled, "after hook should not run on failure")
}

func TestContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}}
	p1 := &hookedPhase{fn: func() error {
		cancel()
		return nil
	}}
	p2 := &hookedPhase{}
	m.SetPhases(Phases{p1, p2})
	require.Error(t, m.Run(ctx))
	require.Contains(t, ctx.Err().Error(), "cancel")

	require.True(t, p1.beforeCalled, "1st before hook was not called")
	require.True(t, p1.afterCalled, "1st after hook was not called")
	require.True(t, p1.runCalled, "1st run was not called")
	// this should happen because the phase was completed before the context was cancelled
	require.True(t, p1.cleanupCalled, "1st cleanup was not called")

	require.False(t, p2.beforeCalled, "2nd before hook was called")
	require.False(t, p2.afterCalled, "2nd after hook was called")
	require.False(t, p2.runCalled, "2nd run was called")
	require.False(t, p2.cleanupCalled, "2nd cleanup was called")
}

// readOnlyPhase reads from hosts and changes nothing, so a dry run runs it as itself.
type readOnlyPhase struct {
	err       error
	runCalled bool
}

func (p *readOnlyPhase) Title() string {
	return "read-only phase"
}

func (p *readOnlyPhase) Explanation() string {
	return "test function"
}

func (p *readOnlyPhase) ReadOnly() string {
	return "test phase, reads nothing"
}

func (p *readOnlyPhase) Run(_ context.Context) error {
	p.runCalled = true
	return p.err
}

// dryRunPhase brings its own dry path, the way Disconnect does.
type dryRunPhase struct {
	runCalled    bool
	dryRunCalled bool
}

func (p *dryRunPhase) Title() string {
	return "dry run phase"
}

func (p *dryRunPhase) Explanation() string {
	return "test function"
}

func (p *dryRunPhase) DryRun() error {
	p.dryRunCalled = true
	return nil
}

func (p *dryRunPhase) Run(_ context.Context) error {
	p.runCalled = true
	return nil
}

// mutatingPhase declares nothing, which is what most phases in the tree do. It is the case the
// gate exists for: a dry run must report it rather than run it.
type mutatingPhase struct {
	runCalled bool
}

func (p *mutatingPhase) Title() string {
	return "mutating phase"
}

func (p *mutatingPhase) Explanation() string {
	return "test function"
}

func (p *mutatingPhase) Run(_ context.Context) error {
	p.runCalled = true
	return nil
}

// TestDryRunSkipsUndeclaredPhases is the regression test for the gate being opt-in. It used to
// read `if dp, ok := p.(withDryRun); ok && m.DryRun`, so a phase that implemented neither
// interface fell through to Run and changed the host it was supposed to be reporting on.
func TestDryRunSkipsUndeclaredPhases(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}, DryRun: true}
	ro := &readOnlyPhase{}
	dry := &dryRunPhase{}
	mut := &mutatingPhase{}
	m.SetPhases(Phases{ro, dry, mut})

	require.NoError(t, m.Run(context.Background()))
	require.True(t, ro.runCalled, "the read-only phase should run as itself under a dry run")
	require.True(t, dry.dryRunCalled, "the phase's own DryRun was not called")
	require.False(t, dry.runCalled, "DryRun ran, so Run should not have")
	require.False(t, mut.runCalled, "a phase declaring neither interface must not run under a dry run")
}

// TestWetRunRunsEveryPhase pins the other direction: the gate is the only thing the marker
// changes, so with DryRun off all three phases run normally.
func TestWetRunRunsEveryPhase(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}}
	ro := &readOnlyPhase{}
	dry := &dryRunPhase{}
	mut := &mutatingPhase{}
	m.SetPhases(Phases{ro, dry, mut})

	require.NoError(t, m.Run(context.Background()))
	require.True(t, ro.runCalled, "read-only run was not called")
	require.True(t, mut.runCalled, "mutating run was not called")
	require.True(t, dry.runCalled, "run was not called")
	require.False(t, dry.dryRunCalled, "DryRun was called outside a dry run")
}

// TestDryRunSkipsHooksAndCleanUp covers the bookkeeping around a skipped phase. Its before hook
// must not fire, because a hook changes the host by the same route the phase would; and it must
// not be cleaned up when a later phase fails, because there is nothing to undo.
func TestDryRunSkipsHooksAndCleanUp(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}, DryRun: true}
	skipped := &hookedPhase{}
	failing := &readOnlyPhase{err: fmt.Errorf("preflight failed")}
	m.SetPhases(Phases{skipped, failing})

	require.Error(t, m.Run(context.Background()))
	require.True(t, failing.runCalled, "the read-only phase did not run")
	require.False(t, skipped.runCalled, "the skipped phase ran")
	require.False(t, skipped.beforeCalled, "the before hook fired for a phase that was skipped")
	require.False(t, skipped.cleanupCalled, "a phase that never ran was cleaned up")
}

// TestLockHasNoDryRunPath pins a deliberate decision rather than an implementation detail.
// Taking the cluster lock is not something to do a half version of, so Lock declares neither
// interface and a dry run skips it. Adding either one to Lock should fail here and be argued
// for, not slipped in.
func TestLockHasNoDryRunPath(t *testing.T) {
	var p Phase = &Lock{}

	_, hasDryRun := p.(withDryRun)
	require.False(t, hasDryRun, "Lock must not be runnable as a dry run: it is critical logic")

	_, isReadOnly := p.(readOnly)
	require.False(t, isReadOnly, "Lock deletes files on hosts, it is not read-only")
}

// TestReadOnlyPhasesAreMarked names the phases a dry run is allowed to run for real. Every one
// only reads: connect, detect the OS, gather facts, validate hosts, read the running version.
// A phase joining this list has to be checked by hand, so the list is worth pinning.
func TestReadOnlyPhasesAreMarked(t *testing.T) {
	for _, p := range []Phase{
		&Connect{},
		&DetectOS{},
		&GatherFacts{},
		&ValidateHosts{},
		&GatherFactsDistro{},
	} {
		_, ok := p.(readOnly)
		require.Truef(t, ok, "%s is not marked read-only, so a dry run would skip it", p.Title())

		// The reason is rendered into docs/phases/<name>.md, so an empty one is a phase that
		// claims to be safe under a dry run without saying why.
		require.NotEmptyf(t, DryRunNote(p), "%s is marked read-only without giving a reason", p.Title())
	}
}

// unpreparablePhase fails in Prepare the way KubeConfig and LabelNodes do against a cluster
// with no controller running. It declares neither dry-run interface, so it lands in the skip
// bucket -- readOnlyUnpreparable below is the same phase on the other side of that gate.
type unpreparablePhase struct {
	prepareErr   error
	runCalled    bool
	prepareCalls int
}

func (p *unpreparablePhase) Title() string {
	return "unpreparable phase"
}

func (p *unpreparablePhase) Explanation() string {
	return "test function"
}

func (p *unpreparablePhase) Prepare(_ context.Context, _ *cluster.Cluster, _ *distro.Distro) error {
	p.prepareCalls++
	return p.prepareErr
}

func (p *unpreparablePhase) Run(_ context.Context) error {
	p.runCalled = true
	return nil
}

// ReadOnly is declared on the type, so the marker has to be switched off through the manager's
// phase list rather than through the interface. readOnlyUnpreparable is the wrapper that keeps
// it; the bare type is used where the phase must land in the skip bucket.
type readOnlyUnpreparable struct{ unpreparablePhase }

func (p *readOnlyUnpreparable) ReadOnly() string {
	return "test phase, reads nothing"
}

// TestDryRunToleratesPrepareFailureOnSkippedPhase pins the behaviour a live cluster found:
// KubeConfig.Prepare returns ErrNoControllers when nothing is running, which under a dry run is
// the state every skipped phase left behind rather than a preflight result. Failing there made
// --dry-run unusable against a cluster with nothing installed on it.
func TestDryRunToleratesPrepareFailureOnSkippedPhase(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}, DryRun: true}
	unprepared := &unpreparablePhase{prepareErr: fmt.Errorf("no controllers are running")}
	// A read-only phase after it, not a mutating one: a mutating phase is skipped under a dry
	// run whether the run continued or not, so it could not tell the two apart.
	after := &readOnlyPhase{}
	m.SetPhases(Phases{unprepared, after})

	require.NoError(t, m.Run(context.Background()), "a skipped phase's Prepare must not fail a dry run")
	require.Equal(t, 1, unprepared.prepareCalls, "Prepare still runs: it is what filters the list")
	require.False(t, unprepared.runCalled, "the phase was skipped, so Run must not have been called")
	require.True(t, after.runCalled, "the run should have continued past the failure")
}

// TestDryRunFailsOnPrepareFailureOfReadOnlyPhase is the other half. A read-only phase runs for
// real under a dry run, so its Prepare failing is the preflight failing and has to stop the run.
func TestDryRunFailsOnPrepareFailureOfReadOnlyPhase(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}, DryRun: true}
	unprepared := &readOnlyUnpreparable{unpreparablePhase{prepareErr: fmt.Errorf("host unreachable")}}
	after := &readOnlyPhase{}
	m.SetPhases(Phases{unprepared, after})

	require.Error(t, m.Run(context.Background()), "a read-only phase failing to prepare is a failed preflight")
	require.False(t, after.runCalled, "the run should have stopped at the failure")
}

// TestWetRunFailsOnPrepareFailure keeps the tolerance out of a real run entirely.
func TestWetRunFailsOnPrepareFailure(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}}
	unprepared := &unpreparablePhase{prepareErr: fmt.Errorf("no controllers are running")}
	after := &readOnlyPhase{}
	m.SetPhases(Phases{unprepared, after})

	require.Error(t, m.Run(context.Background()))
	require.False(t, after.runCalled)
}

// TestClassifyDryRun pins the classifier the docs render. magefiles/pkg/gen/docs/phase.go labels every
// phase in docs/phases/apply.md and reset.md with this, and Run() gates on the same call, so a
// label there is what the phase does rather than a second description of it.
func TestClassifyDryRun(t *testing.T) {
	require.Equal(t, DryRunReadOnly, ClassifyDryRun(&readOnlyPhase{}))
	require.Equal(t, DryRunOwnPath, ClassifyDryRun(&dryRunPhase{}))
	require.Equal(t, DryRunSkip, ClassifyDryRun(&mutatingPhase{}))

	require.Equal(t, "runs, reads only", DryRunReadOnly.String())
	require.Equal(t, "runs its own dry-run path", DryRunOwnPath.String())
	require.Equal(t, "reported, not run", DryRunSkip.String())

	require.Equal(t, "test phase, reads nothing", DryRunNote(&readOnlyPhase{}))
	require.Empty(t, DryRunNote(&dryRunPhase{}), "only read-only phases carry a note")
	require.Empty(t, DryRunNote(&mutatingPhase{}))
}

// changingPhase declares whether it changed anything, which most phases in the tree do not. It
// is configurable so that both answers can be pinned, since "declared, and nothing changed" is a
// different result from "never said".
type changingPhase struct {
	title     string
	changed   bool
	shouldRun bool
	err       error
}

func (p *changingPhase) Title() string {
	if p.title == "" {
		return "changing phase"
	}
	return p.title
}

func (p *changingPhase) Explanation() string {
	return "test function"
}

func (p *changingPhase) ShouldRun() bool {
	return p.shouldRun
}

func (p *changingPhase) Changed() bool {
	return p.changed
}

func (p *changingPhase) Run(_ context.Context) error {
	return p.err
}

// TestRunResultRecordsEveryDisposition pins the three places a phase can end up. Skipped is the
// one worth having a test for: a phase filtered out by ShouldRun runs nothing, but it can have
// finished its work in Prepare, so leaving it out of the result would lose a real change.
func TestRunResultRecordsEveryDisposition(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}}
	m.SetPhases(Phases{
		&changingPhase{title: "ran", shouldRun: true},
		&conditionalPhase{},
		&mutatingPhase{},
	})

	require.NoError(t, m.Run(context.Background()))
	require.Equal(t, []string{"ran", "mutating phase"}, Titles(m.Result.Ran))
	require.Empty(t, Titles(m.Result.Planned))
	require.Equal(t, []string{"conditional phase"}, Titles(m.Result.Skipped))
}

// TestRunResultChangedComesOnlyFromDeclaredPhases is the whole point of the interface being
// opt-in. A phase that says nothing cannot make changed true, and it cannot make it false
// either: it makes the answer partial, and it is named so an operator knows what was not
// covered.
func TestRunResultChangedComesOnlyFromDeclaredPhases(t *testing.T) {
	for _, tc := range []struct {
		name       string
		phases     Phases
		changed    bool
		complete   bool
		undeclared []string
	}{
		{
			name:     "a declared phase that changed nothing",
			phases:   Phases{&changingPhase{shouldRun: true}},
			complete: true,
		},
		{
			name:     "a declared phase that changed something",
			phases:   Phases{&changingPhase{shouldRun: true, changed: true}},
			changed:  true,
			complete: true,
		},
		{
			name:       "a phase that never said",
			phases:     Phases{&mutatingPhase{}},
			undeclared: []string{"mutating phase"},
		},
		{
			name:       "a declared phase beside a silent one",
			phases:     Phases{&changingPhase{shouldRun: true, changed: true}, &mutatingPhase{}},
			changed:    true,
			undeclared: []string{"mutating phase"},
		},
		{
			name:     "a phase skipped by ShouldRun still reports what it changed",
			phases:   Phases{&changingPhase{shouldRun: false, changed: true}},
			changed:  true,
			complete: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}}
			m.SetPhases(tc.phases)

			require.NoError(t, m.Run(context.Background()))
			require.Equal(t, tc.changed, m.Result.Changed())
			require.Equal(t, tc.complete, m.Result.Complete())
			require.Equal(t, tc.undeclared, m.Result.Undeclared())
		})
	}
}

// TestRunResultSurvivesAFailure covers the case the result is read in most: a run that stopped
// part-way. The phases it got through are the ones that changed the fleet, so they have to be in
// the result even though Run returned an error.
func TestRunResultSurvivesAFailure(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}}
	m.SetPhases(Phases{
		&changingPhase{title: "first", shouldRun: true, changed: true},
		&changingPhase{title: "second", shouldRun: true, err: fmt.Errorf("phase failed")},
		&changingPhase{title: "third", shouldRun: true},
	})

	require.Error(t, m.Run(context.Background()))
	require.Equal(t, []string{"first", "second"}, Titles(m.Result.Ran))
	require.True(t, m.Result.Changed())
}

// TestDryRunResultReportsOutstandingWork pins the check-mode reading. A dry run changes nothing,
// so Changed is false and the question worth asking is whether a real run would have: that is
// Outstanding, and a phase only reaches Planned after its ShouldRun said there was work to do.
func TestDryRunResultReportsOutstandingWork(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}, DryRun: true}
	m.SetPhases(Phases{&changingPhase{title: "would sync", shouldRun: true}})

	require.NoError(t, m.Run(context.Background()))
	require.Empty(t, Titles(m.Result.Ran))
	require.Equal(t, []string{"would sync"}, Titles(m.Result.Planned))
	require.False(t, m.Result.Changed(), "a dry run changes nothing")
	require.True(t, m.Result.Outstanding(), "the phase had work to do and was reported instead")
}

// TestDryRunReportsNoOutstandingWorkWhenThereIsNone is the other half: a phase whose ShouldRun
// found nothing to do is skipped before the dry-run gate, so it is never reported as pending.
func TestDryRunReportsNoOutstandingWorkWhenThereIsNone(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}, DryRun: true}
	m.SetPhases(Phases{&changingPhase{title: "nothing drifted", shouldRun: false}})

	require.NoError(t, m.Run(context.Background()))
	require.Empty(t, Titles(m.Result.Planned))
	require.False(t, m.Result.Outstanding())
}

// TestResultSinkCollectsTheRun covers the path the Ansible module mode reads the result through,
// since the command it calls returns an error and nothing else.
func TestResultSinkCollectsTheRun(t *testing.T) {
	ctx, sink := WithResultSink(context.Background())
	require.False(t, sink.Observed(), "nothing has run yet")

	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}}
	m.SetPhases(Phases{&changingPhase{title: "ran", shouldRun: true, changed: true}})
	require.NoError(t, m.Run(ctx))

	require.True(t, sink.Observed())
	require.True(t, sink.Result().Changed())
	require.Equal(t, []string{"ran"}, Titles(sink.Result().Ran))
}

// TestResultSinkObservesNothingBeforeThePhases pins the difference between "no phase changed
// anything" and "no phase ran". A command that failed on its configuration has to read as the
// second, or a playbook is told nothing happened when nothing was ever attempted.
func TestResultSinkObservesNothingBeforeThePhases(t *testing.T) {
	ctx, sink := WithResultSink(context.Background())

	m := Manager{}
	m.SetPhases(Phases{&changingPhase{shouldRun: true, changed: true}})
	require.Error(t, m.Run(ctx))

	require.False(t, sink.Observed())
}

// hostChangingPhase declares which hosts it changed as well as whether it changed anything, so
// the two halves of the opt-in can be exercised apart: a phase can speak fleet-wide and stay
// silent per host, which is what every phase in the tree did before HostsChanged existed.
type hostChangingPhase struct {
	title     string
	changed   bool
	hosts     []HostChange
	shouldRun bool
	runFor    time.Duration
}

func (p *hostChangingPhase) Title() string {
	if p.title == "" {
		return "host changing phase"
	}
	return p.title
}

func (p *hostChangingPhase) Explanation() string {
	return "test function"
}

func (p *hostChangingPhase) ShouldRun() bool {
	return p.shouldRun
}

func (p *hostChangingPhase) Changed() bool {
	return p.changed
}

func (p *hostChangingPhase) HostsChanged() []HostChange {
	return p.hosts
}

func (p *hostChangingPhase) Run(_ context.Context) error {
	time.Sleep(p.runFor)
	return nil
}

// TestPhaseResultRecordsHostChanges pins that HostsDeclared tracks the interface and not the
// contents. A phase that implements HostsChanged and returns nothing has declared that it
// changed no host, which is a different answer from a phase that was never asked.
func TestPhaseResultRecordsHostChanges(t *testing.T) {
	for _, tc := range []struct {
		name     string
		phase    Phase
		declared bool
		hosts    []HostChange
	}{
		{
			name:     "never asked",
			phase:    &changingPhase{changed: true},
			declared: false,
			hosts:    nil,
		},
		{
			name:     "declared, no host moved",
			phase:    &hostChangingPhase{},
			declared: true,
			hosts:    nil,
		},
		{
			name: "declared, one host moved",
			phase: &hostChangingPhase{
				changed: true,
				hosts: []HostChange{
					{
						Host:    "10.0.0.1",
						Changed: true,
						Details: []HostChangeDetail{
							{
								Path:   "/etc/hosts",
								Reason: "out of sync",
							},
						},
					},
				},
			},
			declared: true,
			hosts: []HostChange{
				{
					Host:    "10.0.0.1",
					Changed: true,
					Details: []HostChangeDetail{
						{
							Path:   "/etc/hosts",
							Reason: "out of sync",
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := phaseResult(tc.phase, 0)
			require.Equal(t, tc.declared, got.HostsDeclared)
			require.Equal(t, tc.hosts, got.Hosts)
		})
	}
}

// TestHostsUndeclaredNamesEveryPhaseThatStayedSilent is the per-host counterpart of
// TestRunResultChangedComesOnlyFromDeclaredPhases. A phase that reports fleet-wide but not per
// host is still silent per host, and has to be named: a host missing from a summary built on
// this may simply never have been spoken for.
func TestHostsUndeclaredNamesEveryPhaseThatStayedSilent(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}}
	m.SetPhases(Phases{
		&hostChangingPhase{title: "speaks per host", shouldRun: true},
		&changingPhase{title: "speaks fleet wide", shouldRun: true},
		&mutatingPhase{},
	})

	require.NoError(t, m.Run(context.Background()))
	require.Equal(t, []string{"speaks fleet wide", "mutating phase"}, m.Result.HostsUndeclared())
	require.Equal(t, []string{"mutating phase"}, m.Result.Undeclared())
}

// TestHostSummariesRollUpAcrossPhases pins the denominator. Named alone cannot be read -- a host
// named by one of one reporting phase is fully accounted for and a host named by one of eight is
// not -- so Reporting counts every phase that declared per host, including the ones that named
// no host at all.
func TestHostSummariesRollUpAcrossPhases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result RunResult
		want   []HostSummary
	}{
		{
			name:   "nothing reported",
			result: RunResult{},
			want:   nil,
		},
		{
			name: "one phase reported and named nobody",
			result: RunResult{
				Ran: []PhaseResult{
					{
						Title:         "quiet",
						HostsDeclared: true,
					},
				},
			},
			want: nil,
		},
		{
			name: "a host that moved in one phase of two stays changed",
			result: RunResult{
				Ran: []PhaseResult{
					{
						Title:         "first",
						HostsDeclared: true,
						Hosts: []HostChange{
							{
								Host:    "kc01",
								Changed: true,
							},
							{
								Host:    "kc02",
								Changed: false,
							},
						},
					},
					{
						Title:         "second",
						HostsDeclared: true,
						Hosts: []HostChange{
							{
								Host:    "kc01",
								Changed: false,
							},
						},
					},
				},
			},
			want: []HostSummary{
				{
					Host:      "kc01",
					Changed:   true,
					Named:     2,
					Reporting: 2,
				},
				{
					Host:      "kc02",
					Changed:   false,
					Named:     1,
					Reporting: 2,
				},
			},
		},
		{
			name: "a phase that never declared adds no denominator",
			result: RunResult{
				Ran: []PhaseResult{
					{
						Title:         "declared",
						HostsDeclared: true,
						Hosts: []HostChange{
							{
								Host:    "kc01",
								Changed: true,
							},
						},
					},
					{
						Title:         "silent",
						HostsDeclared: false,
					},
				},
			},
			want: []HostSummary{
				{
					Host:      "kc01",
					Changed:   true,
					Named:     1,
					Reporting: 1,
				},
			},
		},
		{
			name: "planned and skipped phases count too",
			result: RunResult{
				Planned: []PhaseResult{
					{
						Title:         "planned",
						HostsDeclared: true,
						Hosts: []HostChange{
							{
								Host:    "kc01",
								Changed: false,
							},
						},
					},
				},
				Skipped: []PhaseResult{
					{
						Title:         "skipped",
						HostsDeclared: true,
						Hosts: []HostChange{
							{
								Host:    "kc02",
								Changed: true,
							},
						},
					},
				},
			},
			want: []HostSummary{
				{
					Host:      "kc01",
					Changed:   false,
					Named:     1,
					Reporting: 2,
				},
				{
					Host:      "kc02",
					Changed:   true,
					Named:     1,
					Reporting: 2,
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.result.HostSummaries())
		})
	}
}

// TestSignalReportsHowMuchChangedIsWorth pins that the vocabulary lives here rather than in the
// Ansible module, which now aliases it.
func TestSignalReportsHowMuchChangedIsWorth(t *testing.T) {
	complete := RunResult{
		Ran: []PhaseResult{
			{
				Title:    "declared",
				Declared: true,
			},
		},
	}
	require.Equal(t, SignalComplete, complete.Signal())

	partial := RunResult{
		Ran: []PhaseResult{
			{
				Title:    "declared",
				Declared: true,
			},
			{
				Title:    "silent",
				Declared: false,
			},
		},
	}
	require.Equal(t, SignalPartial, partial.Signal())
}

// TestRunRecordsPhaseDuration pins that a phase that executed carries a duration and a phase
// that did not carries zero. The zero is the point: a planned phase took no time because it
// never ran, and a reader comparing durations should see that rather than a missing field.
func TestRunRecordsPhaseDuration(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}}
	m.SetPhases(Phases{
		&hostChangingPhase{title: "ran", shouldRun: true, runFor: 10 * time.Millisecond},
		&conditionalPhase{},
	})

	require.NoError(t, m.Run(context.Background()))
	require.Len(t, m.Result.Ran, 1)
	require.GreaterOrEqual(t, m.Result.Ran[0].Duration, 10*time.Millisecond)
	require.Len(t, m.Result.Skipped, 1)
	require.Zero(t, m.Result.Skipped[0].Duration)
}

// warningPhase is a phase with something to say that is not an error.
type warningPhase struct {
	title    string
	warnings []Warning
	err      error
}

func (p *warningPhase) Title() string {
	if p.title == "" {
		return "warning phase"
	}
	return p.title
}

func (p *warningPhase) Explanation() string {
	return "test function"
}

func (p *warningPhase) Warnings() []Warning {
	return p.warnings
}

func (p *warningPhase) Run(_ context.Context) error {
	return p.err
}

// A phase's warnings reach the run's result with the phase's own title stamped on them, so a
// phase cannot disagree with its title and a reader of a receipt can tell which phase decided
// what. See warningReporter.
func TestRunCollectsPhaseWarnings(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}}
	m.SetPhases(Phases{
		&warningPhase{
			title: "Upload files",
			warnings: []Warning{
				{
					// Deliberately filled in with the wrong phase: the run overwrites it.
					Phase:  "something else",
					Host:   "10.0.0.11",
					Path:   "/var/lib/rancher/k3s/agent/images/vendor.tar",
					Reason: "kept, unmanaged",
				},
			},
		},
		&changingPhase{changed: true},
	})

	require.NoError(t, m.Run(context.Background()))
	require.Equal(t, []Warning{
		{
			Phase:  "Upload files",
			Host:   "10.0.0.11",
			Path:   "/var/lib/rancher/k3s/agent/images/vendor.tar",
			Reason: "kept, unmanaged",
		},
	}, m.Result.Warnings)
}

// A phase that warned and then failed has the same thing to say about the hosts it got through
// first, and the failed run is the one whose log an operator is least likely to still have.
func TestRunKeepsWarningsFromAFailedPhase(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}}
	m.SetPhases(Phases{&warningPhase{
		warnings: []Warning{
			{
				Path:   "/var/lib/rancher/k3s/agent/images/vendor.tar",
				Reason: "kept, unmanaged",
			},
		},
		err: fmt.Errorf("run failed"),
	}})

	require.Error(t, m.Run(context.Background()))
	require.Len(t, m.Result.Warnings, 1)
	require.Equal(t, "warning phase", m.Result.Warnings[0].Phase)
}

// A phase that says nothing contributes nothing, rather than an empty warning.
func TestRunWithoutWarningsReportsNone(t *testing.T) {
	m := Manager{Config: &cluster.Cluster{Spec: cluster.Spec{}}}
	m.SetPhases(Phases{&changingPhase{changed: true}})

	require.NoError(t, m.Run(context.Background()))
	require.Empty(t, m.Result.Warnings)
}
