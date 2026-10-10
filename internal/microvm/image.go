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

// Package microvm brings a fleet of Enterprise Linux virtual machines up and down on the
// developer's own machine, under plain qemu, and renders a ZarfCluster inventory pointing at
// them. It exists because the phases that route on OS family cannot be exercised against a
// container: pkg/phase/21_prepare_selinux.go gates on getenforce reporting Enforcing, which no
// container reports, and that phase is fatal on failure -- so the one branch that can stop an
// apply is the one branch never run. The firewalld backend, fapolicyd, the *-selinux RPM
// scriptlets and the kernel-module reboot are in the same position.
//
// Nothing here needs root, a daemon, or anything installed beyond qemu and mtools, and nothing
// it does outlives Down: the host's own SELinux, firewall and kernel are never touched. See
// docs/dev/microvm.md for the developer-facing walkthrough and
// docs/agent/choice-microvm-backend.md for why qemu rather than Firecracker or libvirt.
package microvm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The base image is pinned by dated name and digest rather than through the
// GenericCloud-Base.latest alias the mirror also publishes. A fleet whose guest userland
// changes under it turns an unrelated phase failure into an afternoon, and the alias moves
// without warning; a release that ships a broken image -- Rocky has shipped a GenericCloud
// build with no cloud-init in it at all -- is then something this package steps over by not
// moving, rather than something every developer hits at once.
const (
	// imageName is the file as published, and as cached.
	imageName = "Rocky-10-GenericCloud-Base-10.2-20260525.0.x86_64.qcow2"

	// imageURL is the mirror path imageName is fetched from.
	imageURL = "https://dl.rockylinux.org/pub/rocky/10/images/x86_64/" + imageName

	// imageSHA256 is the digest published alongside imageName in its .CHECKSUM file.
	imageSHA256 = "9fc9e9ff16888bb68ac39b0392e25c9c92684d50c85f1cce6ab549363bbc4b48"
)

// fetchTimeout bounds the download. The image is roughly 520MiB, so this is generous for a
// working connection and short enough that a mirror which has stopped sending bytes fails
// while the developer is still watching.
const fetchTimeout = 15 * time.Minute

// Fetch returns the path to the cached base image, downloading it first if it is not already
// there. The download is written to a sibling .part file and renamed only once the digest
// matches, so an interrupted fetch leaves nothing a later run would mistake for a complete
// image.
//
// The cache lives outside the repository, under XDG_CACHE_HOME, because it is the one artifact
// here worth keeping across a clean checkout.
func Fetch(ctx context.Context) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating image cache %s: %w", dir, err)
	}

	path := filepath.Join(dir, imageName)
	switch digest, err := fileSHA256(path); {
	case err == nil && digest == imageSHA256:
		return path, nil
	case err == nil:
		// A cached file with the wrong digest is a truncated or corrupted download, not a
		// different pin: the name carries the version. Say what was found rather than
		// silently replacing it, since the fix costs 520MiB of somebody's bandwidth.
		return "", fmt.Errorf("cached image %s has digest %s, want %s: remove it and run again", path, digest, imageSHA256)
	case !errors.Is(err, os.ErrNotExist):
		return "", err
	}

	if err := download(ctx, path); err != nil {
		return "", err
	}
	if err := verifyImageUsable(ctx, path); err != nil {
		return "", err
	}
	return path, nil
}

// cacheDir is where the base image is kept between runs.
func cacheDir() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory for the image cache: %w", err)
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "cargoship", "microvm"), nil
}

// download writes imageURL to path through a .part file, verifying the digest before the
// rename so that path either does not exist or holds the pinned image.
func download(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return fmt.Errorf("building request for %s: %w", imageURL, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", imageURL, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body, nothing to do with a close error
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching %s: %s", imageURL, resp.Status)
	}

	part := path + ".part"
	f, err := os.Create(part)
	if err != nil {
		return fmt.Errorf("creating %s: %w", part, err)
	}
	sum := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(f, sum), resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(part) //nolint:errcheck // the original error is the one worth reporting
		return fmt.Errorf("downloading %s: %w", imageURL, copyErr)
	}
	if closeErr != nil {
		os.Remove(part) //nolint:errcheck // the original error is the one worth reporting
		return fmt.Errorf("closing %s: %w", part, closeErr)
	}

	if digest := hex.EncodeToString(sum.Sum(nil)); digest != imageSHA256 {
		os.Remove(part) //nolint:errcheck // the original error is the one worth reporting
		return fmt.Errorf("downloaded %s has digest %s, want %s", imageURL, digest, imageSHA256)
	}
	if err := os.Rename(part, path); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", part, path, err)
	}
	return nil
}

// fileSHA256 is the hex digest of the file at path.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck // read-only, nothing to do with a close error

	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// verifyImageUsable checks the two things about a freshly downloaded image that a digest
// cannot: that cloud-init is in it, and that its SELinux is set to enforce.
//
// Both have been wrong in a published GenericCloud build, and both fail late and
// unrecognisably if they are not checked here. Without cloud-init the seed is ignored, no key
// is installed, and the only symptom is every node timing out waiting for SSH. With SELinux
// disabled the fleet comes up fine and the phases this package exists to exercise all skip,
// which is worse -- the suite reports coverage it does not have.
//
// This runs once per download rather than on every Up: it is a property of the image, and
// guestfish takes several seconds to start its appliance.
func verifyImageUsable(ctx context.Context, path string) error {
	if _, err := exec.LookPath("guestfish"); err != nil {
		// guestfish ships in guestfs-tools, which is not needed to run a fleet. Skipping the
		// check is better than refusing to fetch, so long as it says so.
		fmt.Fprintf(os.Stderr, "microvm: guestfish not found, skipping image verification of %s\n", filepath.Base(path))
		return nil
	}

	// Both answers are narrowed to one line each -- is-file prints "true" or "false", and the
	// grep holds only the SELINUX= assignment -- so neither can be matched by stray text from
	// the other.
	out, err := exec.CommandContext(ctx, "guestfish", "--ro", "-a", path, "-i",
		"is-file", "/usr/bin/cloud-init", ":", "grep", "^SELINUX=", "/etc/selinux/config").CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspecting %s with guestfish: %w: %s", path, err, out)
	}
	if !hasLine(string(out), "true") {
		return fmt.Errorf("%s has no /usr/bin/cloud-init, so a seed would be ignored: rebuild it with `virt-customize -a %s --install cloud-init`", path, path)
	}
	if !hasLine(string(out), "SELINUX=enforcing") {
		return fmt.Errorf("%s does not set SELINUX=enforcing, which is the whole point of this fleet", path)
	}
	return nil
}

// hasLine reports whether any line of out, ignoring surrounding space, equals want.
func hasLine(out, want string) bool {
	for line := range strings.SplitSeq(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
