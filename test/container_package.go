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

package test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// exampleDir is the directory the shipped distro definitions live under. ContainerSafeDefinition
// mirrors each definition's path below it, so a source that reaches outside its own directory
// still resolves in the copy.
const exampleDir = "example"

// OutsideSource matches a file source that climbs out of the definition's own directory. The
// generated manifests write one source per list entry, unquoted.
// It is exported because the suites assert on what it matched in the copy they built.
var OutsideSource = regexp.MustCompile(`(?m)^\s*-?\s*source:\s*(\.\.[^\s]*)\s*$`)

// ContainerSafeDefinition copies the distro definition at src into a directory under dst and
// removes the settings in unsettableSysctls from the copy, returning the path to build the
// package from. The definitions under example/ are what cargoship ships, so the walks read
// them rather than carrying a fixture of their own, and edit the copy rather than the
// original.
//
// The copy keeps the definition's path below example/ rather than flattening it to a basename.
// The k3s definitions reach three levels up for the files they share with every other k3s
// version -- killall.sh and the two systemd units in example/k3s/core -- so a flattened copy
// resolves "../../../k3s/core" somewhere above dst, where nothing exists. That used to cost
// three warnings and a package quietly missing all three files; staging failures are errors
// now, so it costs a failed build instead. See copyOutsideSources for the other half.
func ContainerSafeDefinition(src, dst string) (string, error) {
	rel, err := filepath.Rel(exampleDir, src)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("distro definition %s is not under %s/, so its relative sources cannot be preserved", src, exampleDir)
	}

	definition := filepath.Join(dst, rel)
	if err := os.CopyFS(definition, os.DirFS(src)); err != nil {
		return "", fmt.Errorf("failed to copy the distro definition at %s: %w", src, err)
	}

	manifest := filepath.Join(definition, "distro.yaml")
	content, err := os.ReadFile(manifest) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("failed to read the copied distro definition: %w", err)
	}

	if err := copyOutsideSources(src, dst, definition, content); err != nil {
		return "", err
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

// copyOutsideSources copies every file the manifest reaches for outside its own directory,
// putting each at the same position relative to the copy as it held relative to the original.
// os.CopyFS only walks the definition directory itself, so a shared tree alongside it --
// example/k3s/core, which every k3s version points at -- is otherwise never copied.
//
// Each destination has to land inside dst. Climbing out of it would mean definition does not sit
// as deep below dst as it did below example/, which is the bug this function exists to prevent;
// writing the file anyway would scatter copies through the filesystem above the temp directory
// and leave the resolved source looking fine to anyone who checked.
//
// A source naming a file that is not there fails here rather than at package-build time. The
// harness is what moved the definition, so a path that no longer resolves is this function's
// bug to report, and reporting it by name beats the build reporting a file it cannot stage.
func copyOutsideSources(src, dst, definition string, content []byte) error {
	copied := map[string]bool{}
	for _, match := range OutsideSource.FindAllSubmatch(content, -1) {
		source := string(match[1])
		if copied[source] {
			continue
		}
		copied[source] = true

		from := filepath.Join(src, source)
		to := filepath.Join(definition, source)
		rel, err := filepath.Rel(dst, to)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("source %s in %s/distro.yaml resolves to %s, outside the copy at %s: the definition was not copied as deep below it as it sits below %s/",
				source, src, to, dst, exampleDir)
		}
		if err := copyFile(from, to); err != nil {
			return fmt.Errorf("failed to copy %s, which %s/distro.yaml reaches outside itself for: %w", from, src, err)
		}
	}
	return nil
}

// copyFile copies one file, creating the directories leading to it and keeping its mode.
func copyFile(from, to string) error {
	info, err := os.Stat(from)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(from) //nolint:gosec
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.WriteFile(to, body, info.Mode().Perm())
}
