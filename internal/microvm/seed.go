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

package microvm

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// seedFiles are the NoCloud documents, named as cloud-init looks for them. The template for
// each is the same name with a .tmpl suffix.
var seedFiles = []string{"user-data", "meta-data", "network-config"} //nolint:gochecknoglobals

// seedBytes is the size of the seed filesystem. A FAT12 image has to be large enough for its
// own metadata before any file fits; 1MiB is the smallest round number that is comfortably
// over that, and the three documents together are under a kilobyte.
const seedBytes = 1 << 20

// seedView is what the templates are rendered against.
type seedView struct {
	Name           string
	Domain         string
	PublicKey      string
	MgmtMAC        string
	LANMAC         string
	PrivateAddress string
	PrefixLen      int
}

// renderSeedFiles renders the three NoCloud documents for a node.
//
// They are rendered separately from being packed so that the rendering is testable without
// mtools and without writing a filesystem: every interesting decision -- which MAC each
// interface matches, which address the private segment gets, which packages make the
// fapolicyd and firewall gates open -- is in the text.
func renderSeedFiles(n Node, publicKey string) (map[string][]byte, error) {
	view := seedView{
		Name:           n.Name,
		Domain:         privateDomain,
		PublicKey:      publicKey,
		MgmtMAC:        n.MgmtMAC,
		LANMAC:         n.LANMAC,
		PrivateAddress: n.PrivateAddress,
		PrefixLen:      privatePrefixLen,
	}

	out := make(map[string][]byte, len(seedFiles))
	for _, name := range seedFiles {
		tmpl, err := template.New(name+".tmpl").ParseFS(templateFS, "templates/"+name+".tmpl")
		if err != nil {
			return nil, fmt.Errorf("parsing the %s template: %w", name, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, view); err != nil {
			return nil, fmt.Errorf("rendering %s for %s: %w", name, n.Name, err)
		}
		out[name] = buf.Bytes()
	}
	return out, nil
}

// writeSeed builds the NoCloud seed image for a node in dir and returns its path.
//
// The seed is a vfat filesystem rather than the ISO9660 one most NoCloud examples use:
// cloud-init accepts either, and building an ISO needs xorriso or genisoimage, neither of
// which is a given on a developer's machine. mkfs.vfat and mcopy come from dosfstools and
// mtools, and mcopy writes the long filenames cloud-init insists on -- it looks for
// `user-data`, which does not fit 8.3.
func writeSeed(dir string, n Node, publicKey string) (string, error) {
	files, err := renderSeedFiles(n, publicKey)
	if err != nil {
		return "", err
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			return "", fmt.Errorf("writing %s for %s: %w", name, n.Name, err)
		}
	}

	path := filepath.Join(dir, "seed.img")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("removing the previous seed %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("creating %s: %w", path, err)
	}
	if err := f.Truncate(seedBytes); err != nil {
		f.Close() //nolint:errcheck // the truncate error is the one worth reporting
		return "", fmt.Errorf("sizing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("closing %s: %w", path, err)
	}

	// The CIDATA label is how cloud-init finds the seed. Without it the datasource is never
	// matched, the node boots with no key, and the only symptom is a wait for SSH that
	// never ends.
	if out, err := exec.Command("mkfs.vfat", "-n", "CIDATA", path).CombinedOutput(); err != nil {
		return "", fmt.Errorf("formatting %s: %w: %s", path, err, out)
	}

	args := []string{"-i", path}
	for _, name := range seedFiles {
		args = append(args, filepath.Join(dir, name))
	}
	args = append(args, "::")
	if out, err := exec.Command("mcopy", args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("packing the seed for %s: %w: %s", n.Name, err, out)
	}
	return path, nil
}
