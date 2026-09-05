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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// unsettableSysctls are the settings the example definitions ask for that a node cannot apply
// when the node is a container, and so the settings every walk strips before it builds its
// package.
//
// Both live under net.netfilter, and the kernel keeps that table for the initial network
// namespace only: a container gets its own namespace, the entries there are not its own, and
// writing them returns EPERM no matter what the container is allowed to do. Privileged does
// not help, and neither does asking Docker to set them at container start -- runc gets the
// same EPERM and refuses to start the machine at all.
//
// The distinction that matters is what happens next, because it is not the same on every
// image. `sysctl --system` reports the refusal either way, but the procps in the Fedora image
// exits 1 for it and the one in the Ubuntu image exits 0, so the prepare phase fails on the
// Fedora nodes and passes on the Ubuntu ones with the same two settings unapplied. Leaving
// them in the package would mean the suite could never get past prepare, and would say
// nothing about cargoship while it failed.
//
// Nothing else is removed. The other twenty settings the example carries do apply inside a
// privileged container -- see the Privileged field on the machines in main_test.go -- so the
// phase still renders, writes and applies a real package's sysctls, on real nodes, and the
// assertions in 20_prepare_host_test.go still read the file back off every host.
var unsettableSysctls = []string{ //nolint:gochecknoglobals
	"net.netfilter.nf_conntrack_max",
	"net.netfilter.nf_conntrack_buckets",
}

// containerSafeDefinition copies the distro definition at src into a directory under dst and
// removes the settings in unsettableSysctls from the copy, returning the path to build the
// package from. The definitions under example/ are what cargoship ships, so the walks read
// them rather than carrying a fixture of their own, and edit the copy rather than the
// original.
func containerSafeDefinition(src, dst string) (string, error) {
	definition := filepath.Join(dst, filepath.Base(src))
	if err := os.CopyFS(definition, os.DirFS(src)); err != nil {
		return "", fmt.Errorf("failed to copy the distro definition at %s: %w", src, err)
	}

	manifest := filepath.Join(definition, "distro.yaml")
	content, err := os.ReadFile(manifest) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("failed to read the copied distro definition: %w", err)
	}

	for _, key := range unsettableSysctls {
		// The settings are one YAML mapping entry per line, quoted, so the whole line goes
		// including the newline that ends it. A key the definition does not carry is not an
		// error: not every example sets all of them.
		entry := regexp.MustCompile(`(?m)^[\t ]*"?` + regexp.QuoteMeta(key) + `"?[\t ]*:.*\n`)
		content = entry.ReplaceAll(content, nil)
	}

	// os.CopyFS gives the copy whatever mode the source had, which is not necessarily
	// writable, so make it writable before rewriting it.
	if err := os.Chmod(manifest, 0o600); err != nil {
		return "", fmt.Errorf("failed to make the copied distro definition writable: %w", err)
	}
	if err := os.WriteFile(manifest, content, 0o600); err != nil {
		return "", fmt.Errorf("failed to rewrite the copied distro definition: %w", err)
	}
	return definition, nil
}
