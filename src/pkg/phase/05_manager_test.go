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

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
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
	m := Manager{Config: &cluster.ZarfCluster{Spec: cluster.ZarfClusterSpec{}}}
	p := &conditionalPhase{}
	m.AddPhase(p)
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

func (p *configPhase) Prepare(_ context.Context, c *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	p.receivedConfig = c != nil
	return nil
}

func (p *configPhase) Run(_ context.Context) error {
	return nil
}

func TestConfigPhase(t *testing.T) {
	m := Manager{Config: &cluster.ZarfCluster{Spec: cluster.ZarfClusterSpec{}}}
	p := &configPhase{}
	m.AddPhase(p)
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
	m := Manager{Config: &cluster.ZarfCluster{Spec: cluster.ZarfClusterSpec{}}}
	p := &hookedPhase{}
	m.AddPhase(p)
	require.Error(t, m.Run(context.Background()))
	require.True(t, p.beforeCalled, "before hook was not called")
	require.False(t, p.afterCalled, "after hook should not run on failure")
}

func TestContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := Manager{Config: &cluster.ZarfCluster{Spec: cluster.ZarfClusterSpec{}}}
	p1 := &hookedPhase{fn: func() error {
		cancel()
		return nil
	}}
	p2 := &hookedPhase{}
	m.AddPhase(p1, p2)
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

func (p *readOnlyPhase) ReadOnly() {}

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
	m := Manager{Config: &cluster.ZarfCluster{Spec: cluster.ZarfClusterSpec{}}, DryRun: true}
	ro := &readOnlyPhase{}
	dry := &dryRunPhase{}
	mut := &mutatingPhase{}
	m.AddPhase(ro, dry, mut)

	require.NoError(t, m.Run(context.Background()))
	require.True(t, ro.runCalled, "the read-only phase should run as itself under a dry run")
	require.True(t, dry.dryRunCalled, "the phase's own DryRun was not called")
	require.False(t, dry.runCalled, "DryRun ran, so Run should not have")
	require.False(t, mut.runCalled, "a phase declaring neither interface must not run under a dry run")
}

// TestWetRunRunsEveryPhase pins the other direction: the gate is the only thing the marker
// changes, so with DryRun off all three phases run normally.
func TestWetRunRunsEveryPhase(t *testing.T) {
	m := Manager{Config: &cluster.ZarfCluster{Spec: cluster.ZarfClusterSpec{}}}
	ro := &readOnlyPhase{}
	dry := &dryRunPhase{}
	mut := &mutatingPhase{}
	m.AddPhase(ro, dry, mut)

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
	m := Manager{Config: &cluster.ZarfCluster{Spec: cluster.ZarfClusterSpec{}}, DryRun: true}
	skipped := &hookedPhase{}
	failing := &readOnlyPhase{err: fmt.Errorf("preflight failed")}
	m.AddPhase(skipped, failing)

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

func (p *unpreparablePhase) Prepare(_ context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
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

func (p *readOnlyUnpreparable) ReadOnly() {}

// TestDryRunToleratesPrepareFailureOnSkippedPhase pins the behaviour a live cluster found:
// KubeConfig.Prepare returns ErrNoControllers when nothing is running, which under a dry run is
// the state every skipped phase left behind rather than a preflight result. Failing there made
// --dry-run unusable against a cluster with nothing installed on it.
func TestDryRunToleratesPrepareFailureOnSkippedPhase(t *testing.T) {
	m := Manager{Config: &cluster.ZarfCluster{Spec: cluster.ZarfClusterSpec{}}, DryRun: true}
	unprepared := &unpreparablePhase{prepareErr: fmt.Errorf("no controllers are running")}
	// A read-only phase after it, not a mutating one: a mutating phase is skipped under a dry
	// run whether the run continued or not, so it could not tell the two apart.
	after := &readOnlyPhase{}
	m.AddPhase(unprepared, after)

	require.NoError(t, m.Run(context.Background()), "a skipped phase's Prepare must not fail a dry run")
	require.Equal(t, 1, unprepared.prepareCalls, "Prepare still runs: it is what filters the list")
	require.False(t, unprepared.runCalled, "the phase was skipped, so Run must not have been called")
	require.True(t, after.runCalled, "the run should have continued past the failure")
}

// TestDryRunFailsOnPrepareFailureOfReadOnlyPhase is the other half. A read-only phase runs for
// real under a dry run, so its Prepare failing is the preflight failing and has to stop the run.
func TestDryRunFailsOnPrepareFailureOfReadOnlyPhase(t *testing.T) {
	m := Manager{Config: &cluster.ZarfCluster{Spec: cluster.ZarfClusterSpec{}}, DryRun: true}
	unprepared := &readOnlyUnpreparable{unpreparablePhase{prepareErr: fmt.Errorf("host unreachable")}}
	after := &readOnlyPhase{}
	m.AddPhase(unprepared, after)

	require.Error(t, m.Run(context.Background()), "a read-only phase failing to prepare is a failed preflight")
	require.False(t, after.runCalled, "the run should have stopped at the failure")
}

// TestWetRunFailsOnPrepareFailure keeps the tolerance out of a real run entirely.
func TestWetRunFailsOnPrepareFailure(t *testing.T) {
	m := Manager{Config: &cluster.ZarfCluster{Spec: cluster.ZarfClusterSpec{}}}
	unprepared := &unpreparablePhase{prepareErr: fmt.Errorf("no controllers are running")}
	after := &readOnlyPhase{}
	m.AddPhase(unprepared, after)

	require.Error(t, m.Run(context.Background()))
	require.False(t, after.runCalled)
}
