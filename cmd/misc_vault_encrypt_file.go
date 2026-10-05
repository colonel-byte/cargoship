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
	"fmt"
	"os"
	"strings"

	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/internal/clustercfg"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type vaultEncryptFileOptions struct {
	keyFlags
	dryRun bool
	force  bool
}

func newVaultEncryptFileCommand() *cobra.Command {
	o := vaultEncryptFileOptions{}

	cmd := &cobra.Command{
		Use:     "encrypt-file FILE",
		Args:    cobra.ExactArgs(1),
		Short:   lang.CmdVaultEncryptFileShort,
		Long:    lang.CmdVaultEncryptFileLong,
		Example: lang.CmdVaultEncryptFileExample,
		RunE:    o.run,
	}

	// Not marked required, for the same reason as on encrypt: the environment variables
	// ResolveVaultPassword falls back to are unreachable if cobra rejects the command first.
	cmd.Flags().StringVar(&o.vaultPasswordFile, MiscVaultPasswordFile, "", lang.CmdVaultEncryptFlagPasswordFile)
	addAgeFlags(cmd, &o.keyFlags)
	cmd.Flags().BoolVar(&o.dryRun, InstallDryRun, false, lang.CmdVaultEncryptFileFlagDryRun)
	cmd.Flags().BoolVar(&o.force, MiscVaultForce, false, lang.CmdVaultEncryptFileFlagForce)

	return cmd
}

func (o *vaultEncryptFileOptions) run(cmd *cobra.Command, args []string) error {
	file := args[0]

	keyring, err := o.requireKeyring(cmd)
	if err != nil {
		return err
	}

	src, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}

	encrypted, changed, skipped, err := clustercfg.EncryptConfig(src, keyring, o.force)
	if err != nil {
		return err
	}

	reportRecipientRecord(cmd, file, src, encrypted, keyring, changed, skipped)

	return finishVaultFile(cmd, file, encrypted, changed, skipped, o.dryRun, "encrypted", "nothing to encrypt: every registry credential is encrypted already, or there are none to encrypt")
}

// reportRecipientRecord says what the file's own record of its age recipients has to say about this
// run, given the document as it was read and as it will be written.
//
// It lives here rather than in finishVaultFile because decrypt-file and rekey have nothing to say
// about it: one removes the record and the other rewrites it, and each of those is the operator
// getting exactly what they asked for. It is called before finishVaultFile so that it lands ahead
// of the dry-run branch and ahead of the "nothing to encrypt" return -- a run that changed nothing
// is precisely the run this exists for, which is the same reason the skips are reported there.
//
// Nothing here checks the ciphertext, and nothing here could. The record is a claim the document
// makes about itself; an age header names no recipient, so what is compared is what the file says
// it was encrypted to against what the operator just named. That is worth reporting and is not
// worth acting on -- see docs/agent/choice-age-encryption.md.
func reportRecipientRecord(cmd *cobra.Command, file string, src, encrypted []byte, keyring *clustercfg.Keyring, changed []string, skipped []clustercfg.Skip) {
	l := logger.From(cmd.Context())

	if format, err := keyring.EncryptFormat(); err != nil || format != clustercfg.FormatAge {
		return
	}

	// Read from the document as it was, not as it will be written: a run that encrypted something
	// has already replaced the record with the recipients it was given, so the file as it stands is
	// the only thing that still says what it held before.
	recorded, _, ok, err := clustercfg.RecordedRecipients(src)
	if err != nil {
		l.Warn("could not read the age recipients this file records; the credentials in it were encrypted regardless",
			"file", file, "error", err)
		return
	}

	if len(changed) > 0 {
		if _, _, landed, err := clustercfg.RecordedRecipients(encrypted); err != nil || !landed {
			l.Warn("could not record the age recipients in this file, so it will not say which keys open it; a metadata block written in flow style -- \"{name: x}\" -- has nowhere to put the record",
				"file", file)
		}
	}

	// A skip is a credential this run did not put onto the keys it was given, so skips are what
	// make the recorded set worth reporting: with none of them, every credential in the file is
	// encrypted to the recipients just named and the record agrees with itself.
	named := keyring.RecipientStrings()
	if !ok || len(skipped) == 0 || clustercfg.SameRecipients(recorded, named) {
		return
	}

	// The skip warnings say each credential was left as it was and that cargoship cannot tell what
	// it is encrypted to. This says what the file claims about that, which is the part they cannot.
	message := "the credentials this run left as they were are recorded as encrypted to age recipients other than the ones you named"
	if len(changed) == 0 {
		message = "the age recipients you named are not the ones this file records, and nothing was re-encrypted"
	}
	l.Warn(message,
		"file", file,
		"recorded", strings.Join(recorded, ", "),
		"named", strings.Join(named, ", "),
		"hint", "run 'cargoship vault rekey' with an age identity and these recipients to re-encrypt it to them")
}

// finishVaultFile reports what a whole-file rewrite did and writes the result, which encrypt-file,
// decrypt-file and rekey all do the same way.
//
// A run that changed nothing still prints the document under --dry-run, so that piping the output
// somewhere does not depend on whether there happened to be anything to do.
//
// Skips are warned about before either of those, and that placement is the point of reporting them
// at all. A run that changed nothing is exactly where a mistake hides -- asking for new age
// recipients against a file that is already encrypted does nothing, and without this it would say
// so in the quietest way the command has. Warnings go to stderr through the logger, so a dry run
// piped to a file still gets a clean document.
func finishVaultFile(cmd *cobra.Command, file string, doc []byte, changed []string, skipped []clustercfg.Skip, dryRun bool, verb, nothingToDo string) error {
	l := logger.From(cmd.Context())

	for _, skip := range skipped {
		l.Warn("left this credential as it was: it "+skip.Reason, "file", file, "path", skip.Path)
	}

	// These commands resolve anchors, aliases and merge keys for themselves, and resolve more of
	// them than the decoder an apply reads the file with does -- a merge key that overrides a key
	// it merges, or one naming a list of mappings, are both shapes this rewrite handles and that
	// decoder refuses. Getting the credentials out of the clear is still the right thing to do with
	// such a file, but the operator should hear now that it will not apply, rather than at apply
	// time with no clue which of the two commands to believe.
	if _, err := clustercfg.Parse(cmd.Context(), doc); err != nil {
		l.Warn("this file does not load as a cluster configuration, so an apply will refuse it; the credentials in it were still "+verb,
			"file", file, "error", err)
	}

	if dryRun {
		_, err := cmd.OutOrStdout().Write(doc)
		return err
	}

	if len(changed) == 0 {
		l.Info(nothingToDo, "file", file)
		return nil
	}

	if err := writeFileInPlace(file, doc); err != nil {
		return err
	}
	for _, path := range changed {
		l.Info(verb+" value in place", "file", file, "path", path)
	}
	l.Info(verb+" registry credentials", "file", file, "count", len(changed))
	return nil
}
