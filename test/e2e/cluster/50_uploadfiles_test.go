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

package cluster

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	apicluster "github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/phase"
)

// uploadManifestPath is where the upload phases record what they put on a host. The phase
// package keeps the constant unexported, so the path is repeated here on purpose: if it ever
// moves, this test should fail rather than quietly stop checking anything.
const uploadManifestPath = "/var/lib/cargoship/manifest.txt"

// uploadFiles covers phase/50_uploadfiles.go. It stages the package's images and
// files on the nodes and records each one in the upload manifest, so the assertion is that
// the manifest exists, is not empty, and that every path it claims is really on the host.
//
// Both walks assert the same thing here, so the body is shared: see phaseWalk.
func (s *phaseWalk) uploadFiles() {
	s.T().Helper()

	p := &phase.UploadFiles{}
	s.runPhase(p)
	s.Require().True(ran(p), "nothing to upload from a package that carries images")

	for _, host := range s.harness.hosts() {
		entries := s.manifestOn(host)
		s.Require().NotEmptyf(entries, "%s: upload manifest is empty", host)

		for _, path := range entries {
			s.Require().Truef(host.FileExist(path),
				"%s: manifest claims %s but it is not on the host", host, path)
		}

		s.requireNoOrphanImages(host, entries)
	}
}

// requireNoOrphanImages asserts the engine's image directory holds nothing the upload manifest
// names. The manifest is the only record of what cargoship put there, and cleanStaleUploads
// prunes by comparing it against what the run uploaded -- so a tarball the manifest has lost
// track of is a file nothing will ever remove.
//
// This is what makes the tarball names carrying their image tag safe: a version bump writes a
// new path instead of overwriting one, and it is the prune rather than the name that keeps the
// superseded image off the host. See docs/agent/choice-image-tarball-names.md.
func (s *phaseWalk) requireNoOrphanImages(host *apicluster.Host, entries []string) {
	s.T().Helper()

	dir := s.harness.manager.Distro.Spec.Config.ImagesConfig.Path
	if dir == "" {
		return
	}

	out, err := host.Sudo().ExecOutput("ls -1 " + dir + " 2>/dev/null; true")
	s.Require().NoErrorf(err, "%s: failed to list %s", host, dir)

	for _, name := range strings.Fields(out) {
		s.Require().Containsf(entries, dir+"/"+name,
			"%s: %s/%s is on the host and the manifest does not name it, so no run will prune it", host, dir, name)
	}
}

// uploadRoundTrip runs phase 50 a second time against hosts it has already staged, which has
// to change nothing at all.
//
// The phase re-exports every image into the same path under the package's tarball directory, so
// the second export lands on a file that is already there. Opened for appending, that produces a
// local file with a second archive concatenated onto the first -- a different sha256, which is
// what GenericPhase.uploadFile compares -- and so every image in the package is uploaded again
// on every apply, with the wrong bytes. Opened with O_TRUNC it produces the same file, the
// comparison matches, and the upload is skipped.
//
// Both halves are asserted, because they fail separately: the local digests catch the export,
// and the remote digests catch what the comparison then decided to do about it.
func (s *phaseWalk) uploadRoundTrip() {
	s.T().Helper()

	localBefore := s.localTarballDigests()
	s.Require().NotEmpty(localBefore, "the walk's package carries images, so the export left tarballs")

	remoteBefore := map[string]map[string]string{}
	for _, host := range s.harness.hosts() {
		remoteBefore[host.String()] = s.digestsOn(host, s.manifestOn(host))
	}

	s.runPhase(&phase.UploadFiles{})

	s.Require().Equal(localBefore, s.localTarballDigests(),
		"a re-export changed the image tarballs it had already written, so every image uploads again")

	for _, host := range s.harness.hosts() {
		s.Require().Equal(remoteBefore[host.String()], s.digestsOn(host, s.manifestOn(host)),
			"%s: a second upload of the same package changed what is on the host", host)
	}
}

// localTarballDigests returns the sha256 of every image tarball the export wrote, keyed by path.
func (s *phaseWalk) localTarballDigests() map[string]string {
	s.T().Helper()

	paths, err := filepath.Glob(filepath.Join(s.harness.manager.TempDirectory, config.TarBallDir, "*", "*"))
	s.Require().NoError(err)

	digests := make(map[string]string, len(paths))
	for _, path := range paths {
		f, err := os.Open(path) //nolint:gosec // a path this test just globbed out of its own temp dir
		s.Require().NoError(err)
		sum := sha256.New()
		_, err = io.Copy(sum, f)
		s.Require().NoError(err)
		s.Require().NoError(f.Close())
		digests[path] = hex.EncodeToString(sum.Sum(nil))
	}
	return digests
}

// digestsOn returns the sha256 of each of paths on host, keyed by path. It is how the test sees
// what an upload did or did not replace, and it is deliberately the same comparison
// Host.FileChanged makes before deciding to upload at all.
func (s *phaseWalk) digestsOn(host *apicluster.Host, paths []string) map[string]string {
	s.T().Helper()

	if len(paths) == 0 {
		return map[string]string{}
	}
	out, err := host.Sudo().ExecOutput("sha256sum " + strings.Join(paths, " "))
	s.Require().NoErrorf(err, "%s: failed to checksum the staged files", host)

	digests := make(map[string]string, len(paths))
	for _, line := range strings.Split(out, "\n") {
		sum, path, ok := strings.Cut(strings.TrimSpace(line), "  ")
		if !ok {
			continue
		}
		digests[path] = sum
	}
	s.Require().Lenf(digests, len(paths), "%s: checksummed %d of %d staged files", host, len(digests), len(paths))
	return digests
}

// requireKeptStagedFiles asserts an upload phase left the manifest naming everything an earlier
// phase staged. An entry may leave it only when the file left the host with it: an upgrade
// deletes the version it replaces and prunes those entries, which is the one legitimate way the
// manifest shrinks. A manifest a phase clobbered still fails here, because it drops entries whose
// files are sitting on the host.
func (s *phaseWalk) requireKeptStagedFiles(host *apicluster.Host, before, after []string, upload string) {
	s.T().Helper()

	for _, path := range before {
		if slices.Contains(after, path) {
			continue
		}
		s.Require().Falsef(s.stagedOrPresent(host, path),
			"%s: the %s phase dropped %s from the manifest while the file is still on the host", host, upload, path)
	}
}

// manifestOn returns the paths the upload manifest on host records, one per line as
// "category\tpath".
func (s *phaseWalk) manifestOn(host *apicluster.Host) []string {
	s.T().Helper()

	if !host.FileExist(uploadManifestPath) {
		return nil
	}
	content, err := host.ReadFile(uploadManifestPath)
	s.Require().NoError(err)

	var paths []string
	for _, line := range strings.Split(content, "\n") {
		_, path, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || path == "" {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// Test_50_UploadFiles stages the package on the hosts for the install.
func (s *ApplyPhaseSuite) Test_50_UploadFiles() {
	s.uploadFiles()
}

// Test_50_UploadFiles stages the newer package over what the install left, which is what
// gives the upgrade phases something newer to install.
func (s *UpgradePhaseSuite) Test_50_UploadFiles() {
	s.uploadFiles()
}

// Test_50_UploadFiles stages the package on the joining machine, and re-stages it on the nodes
// that already carry it: apply makes no distinction between a host that has the files and a
// host that does not, so neither does the join walk.
func (s *JoinPhaseSuite) Test_50_UploadFiles() {
	s.uploadFiles()
}

// uploadSweep covers the other half of the image directory being cargoship's to manage: a
// tarball in it that no manifest names is removed, unless the host carries KeepImagesMarker, in
// which case it is kept and warned about.
//
// It plants the files itself rather than waiting for a package to produce them, because what it
// is testing is the sweep's treatment of a file it did not upload -- which is exactly what a
// package cannot produce.
func (s *phaseWalk) uploadSweep() {
	s.T().Helper()

	dir := s.harness.manager.Distro.Spec.Config.ImagesConfig.Path
	s.Require().NotEmpty(dir, "this package configures no image directory, so there is nothing to sweep")

	const planted = "e2e-unmanaged.tar"
	marker := dir + "/" + phase.KeepImagesMarker

	for _, host := range s.harness.hosts() {
		s.Require().NoError(host.WriteFile(dir+"/"+planted, "not a real tarball\n", "0644"))
	}

	s.runPhase(&phase.UploadFiles{})

	for _, host := range s.harness.hosts() {
		s.Require().Falsef(host.FileExist(dir+"/"+planted),
			"%s: %s/%s is not in the manifest and the sweep left it there", host, dir, planted)
		s.requireNoOrphanImages(host, s.manifestOn(host))
	}

	// The escape hatch, on the same hosts: the marker is what a fleet where something else also
	// puts images in this directory uses to keep them.
	for _, host := range s.harness.hosts() {
		s.Require().NoError(host.WriteFile(marker, "", "0644"))
		s.Require().NoError(host.WriteFile(dir+"/"+planted, "not a real tarball\n", "0644"))
	}

	p := &phase.UploadFiles{}
	s.runPhase(p)

	for _, host := range s.harness.hosts() {
		s.Require().Truef(host.FileExist(dir+"/"+planted),
			"%s: %s is present and the sweep removed %s anyway", host, phase.KeepImagesMarker, planted)
	}

	// Every kept file is warned about, once, naming the host and the path -- which is what
	// reaches the run's recap and the receipt.
	var kept []string
	for _, w := range p.Warnings() {
		s.Require().Equal("kept, unmanaged", w.Reason)
		s.Require().Equal(dir+"/"+planted, w.Path)
		kept = append(kept, w.Host)
	}
	s.Require().Len(kept, len(s.harness.hosts()), "every host that kept a tarball should have warned about it")

	// Left as the walk found it: the marker would change what every later phase does here, and
	// the planted file is not something a later assertion should have to know about.
	for _, host := range s.harness.hosts() {
		s.Require().NoError(host.DeleteFile(marker))
		s.Require().NoError(host.DeleteFile(dir + "/" + planted))
	}
}

// Test_50b_UploadRoundTrip re-runs the upload against the hosts Test_50_UploadFiles staged, and
// asserts the second pass is a no-op.
//
// It is gated on the stage-only run rather than added to every walk because it uploads the whole
// package a second time: the staging half of the suite is the one that exists to cover the phases
// before the engine starts, it is what CI runs on every pull request, and the engine phases have
// nothing to say about this.
func (s *ApplyPhaseSuite) Test_50b_UploadRoundTrip() {
	if !stageOnly() {
		s.T().Skipf("%s is not set: the round trip uploads the package twice, so the full walk skips it", stageOnlyEnvVar)
	}
	s.uploadRoundTrip()
}

// Test_50c_UploadSweep covers the sweep of image tarballs cargoship did not upload, and the
// marker file that turns it off. Gated with the round trip above, and for the same reason.
func (s *ApplyPhaseSuite) Test_50c_UploadSweep() {
	if !stageOnly() {
		s.T().Skipf("%s is not set: this plants files on every host, so the full walk skips it", stageOnlyEnvVar)
	}
	s.uploadSweep()
}
