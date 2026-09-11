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

package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/internal/clustercfg"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// MiscVaultForce flag
const MiscVaultForce = "force"

type vaultEncryptPathOptions struct {
	vaultPasswordFile string
	dryRun            bool
	force             bool
}

func newVaultEncryptPathCommand() *cobra.Command {
	o := vaultEncryptPathOptions{}

	cmd := &cobra.Command{
		Use:     "encrypt-path FILE YAML_PATH [YAML_PATH...]",
		Args:    cobra.MinimumNArgs(2),
		Short:   lang.CmdVaultEncryptPathShort,
		Long:    lang.CmdVaultEncryptPathLong,
		Example: lang.CmdVaultEncryptPathExample,
		RunE:    o.run,
	}

	// Not marked required, for the same reason as on vault-encrypt: the environment variables
	// ResolveVaultPassword falls back to are unreachable if cobra rejects the command first.
	cmd.Flags().StringVar(&o.vaultPasswordFile, MiscVaultPasswordFile, "", lang.CmdVaultEncryptFlagPasswordFile)
	cmd.Flags().BoolVar(&o.dryRun, InstallDryRun, false, lang.CmdVaultEncryptPathFlagDryRun)
	cmd.Flags().BoolVar(&o.force, MiscVaultForce, false, lang.CmdVaultEncryptPathFlagForce)

	return cmd
}

func (o *vaultEncryptPathOptions) run(cmd *cobra.Command, args []string) error {
	file := args[0]

	password, err := requireVaultPassword(o.vaultPasswordFile)
	if err != nil {
		return err
	}

	paths, err := canonicalYAMLPaths(args[1:])
	if err != nil {
		return err
	}

	src, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}

	// Every path is encrypted into the document in memory, and the file is written once at the end,
	// so a path that turns out to be missing or encrypted already leaves FILE exactly as it was
	// rather than half done.
	doc := src
	for _, path := range paths {
		doc, err = clustercfg.EncryptAtPath(doc, path, password, o.force)
		if errors.Is(err, clustercfg.ErrAlreadyEncrypted) {
			// --force belongs to the command, so the suggestion to use it is added here rather than
			// carried in the error the encrypting package returns.
			return fmt.Errorf("%w; pass --%s to wrap it again", err, MiscVaultForce)
		}
		if err != nil {
			return err
		}
	}

	// Warnings are held until every path has encrypted, so that a run which writes nothing does not
	// warn about values it left alone.
	for _, path := range paths {
		if !clustercfg.PathIsDecryptable(path) {
			logger.From(cmd.Context()).Warn("cargoship does not decrypt this field at apply time, so the value will reach the host as ciphertext; it decrypts a registry's auth.user, auth.pass, auth.token and tls.ca", "path", path)
		}
	}

	if o.dryRun {
		_, err := cmd.OutOrStdout().Write(doc)
		return err
	}

	if err := writeFileInPlace(file, doc); err != nil {
		return err
	}
	for _, path := range paths {
		logger.From(cmd.Context()).Info("encrypted value in place", "file", file, "path", path)
	}
	return nil
}

// canonicalYAMLPaths returns the YAML paths named on the command line as go-yaml spells them, which
// is the spelling the logs and errors should quote, and rejects a path named twice.
//
// A repeated path is always a mistake and never a harmless one: encrypt-path would report it as
// already encrypted the second time around, and under --force would quietly wrap it twice, which
// produces a value nothing reads back.
func canonicalYAMLPaths(paths []string) ([]string, error) {
	canonical := make([]string, 0, len(paths))
	seen := make(map[string]string, len(paths))
	for _, path := range paths {
		normalized, err := clustercfg.CanonicalYAMLPath(path)
		if err != nil {
			return nil, err
		}
		if first, ok := seen[normalized]; ok {
			return nil, fmt.Errorf("%s and %s name the same value; give each YAML path once", first, path)
		}
		seen[normalized] = path
		canonical = append(canonical, normalized)
	}
	return canonical, nil
}

// writeFileInPlace replaces the contents of name with data, keeping its mode. The new contents go
// to a temporary file alongside it first, so that a failure part-way through cannot leave an
// operator with a half-written cluster configuration and no copy of the original.
func writeFileInPlace(name string, data []byte) error {
	info, err := os.Stat(name)
	if err != nil {
		return fmt.Errorf("inspecting %s: %w", name, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(name), filepath.Base(name)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating a temporary file beside %s: %w", name, err)
	}
	defer func() {
		// Only fires on the failure paths; the rename below has taken the file by then.
		_ = os.Remove(tmp.Name()) //nolint:errcheck // cleanup after a failure that is already being reported
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() //nolint:errcheck // the write error below is the one worth reporting
		return fmt.Errorf("writing %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", tmp.Name(), err)
	}
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return fmt.Errorf("setting the mode on %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), name); err != nil {
		return fmt.Errorf("replacing %s: %w", name, err)
	}
	return nil
}
