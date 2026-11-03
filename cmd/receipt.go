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
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/internal/receipt"
	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// registerFlagReceiptPath adds the run receipt flag to cmd, backed by path.
//
// The default comes from `.distro.receipt_path`, so a fleet that keeps an audit trail configures
// it once rather than restating it on every invocation. The configured value is a template -- see
// expandReceiptPath -- which is what keeps a single setting from having each run overwrite the
// receipt of the last.
func registerFlagReceiptPath(cmd *cobra.Command, path *string) {
	cmd.Flags().StringVar(path, InstallReceiptPath, resolvedConfig.DistroOpts.ReceiptPath, lang.CmdInstallFlagReceiptPath)
	if err := cmd.MarkFlagFilename(InstallReceiptPath, "json"); err != nil {
		logger.Default().Debug("unable to mark the receipt flag as a filename", "error", err)
	}
}

// receiptPathPlaceholder matches one {...} group in a receipt path, including an empty or
// unknown one, so expandReceiptPath can reject what it does not recognise rather than leaving
// it in the filename.
var receiptPathPlaceholder = regexp.MustCompile(`\{[^{}]*\}`)

// receiptTimestampLayout is the {timestamp} expansion: sortable, no characters that need quoting
// in a shell, and second resolution, which is finer than two runs against one fleet are ever
// started apart.
const receiptTimestampLayout = "20060102-150405"

// expandReceiptPath substitutes the placeholders in a receipt path.
//
// A path is templated because it is also a config-file setting. `--receipt-path` names one file
// for one run, and a literal path is the right thing there; `.distro.receipt_path` is read by
// every apply, prepare, reset and engine-config-sync the operator ever runs, and a literal path
// there would have each run truncate the receipt of the one before it -- which is the opposite of
// an audit trail, and silent. {command} and {timestamp} together make a name unique per run, so
// one configured value behaves the way an operator who configured it meant.
//
// An unrecognised placeholder is an error rather than a literal. The alternative is a run that
// succeeds and leaves its record in a file called `apply-{date}.json`, which nobody notices until
// they go looking for a receipt that was never written where they expected it.
func expandReceiptPath(path, command string, started time.Time) (string, error) {
	if path == "" {
		return "", nil
	}

	replacements := map[string]string{
		"{command}":   command,
		"{timestamp}": started.Format(receiptTimestampLayout),
	}

	var unknown []string
	expanded := receiptPathPlaceholder.ReplaceAllStringFunc(path, func(match string) string {
		value, ok := replacements[match]
		if !ok {
			unknown = append(unknown, match)
			return match
		}
		return value
	})
	if len(unknown) > 0 {
		return "", fmt.Errorf("unknown placeholder %s in receipt path %q, expected {command} or {timestamp}", strings.Join(unknown, ", "), path)
	}

	return expanded, nil
}

// receiptFinisher writes the receipt for a run that has ended. It is what startReceipt hands
// back, and is safe to call with a nil manager and a nil package: a run that failed before either
// was built still has a receipt worth writing, naming the command, the error, and nothing it
// never learned.
type receiptFinisher func(manager *phase.Manager, distroPath string, pkg *layout.DistroLayout, runErr error)

// startReceipt prepares a run to be recorded, returning a context carrying the result sink and
// the function that finishes the record.
//
// The sink is installed whether or not --receipt-path was given, because the per-host summary
// table is printed either way and is built from the same result. It costs a mutex and a struct on
// a run that uses neither.
//
// The sink goes on the context rather than being threaded back through cobra, the action and the
// manager, which is the same reason the Ansible module mode reads its results this way -- see
// phase.WithResultSink.
//
// It fails, before the run starts, on a receipt path it cannot expand. A receipt is asked for by
// somebody who needs the record, and the run is repeatable -- so refusing the invocation is
// cheaper than completing a fleet-wide change and then reporting that its record went somewhere
// nobody will look.
func startReceipt(ctx context.Context, cmd *cobra.Command, opt InstallCommon) (context.Context, receiptFinisher, error) {
	started := time.Now()
	command := cmd.Name()

	path, err := expandReceiptPath(opt.receiptPath, command, started)
	if err != nil {
		return ctx, nil, err
	}

	ctx, sink := phase.WithResultSink(ctx)

	// Read os.Args now rather than in the finisher, so the record is of what was invoked even
	// if something later rewrites them.
	var args []string
	if len(os.Args) > 1 {
		args = append(args, os.Args[1:]...)
	}

	finish := func(manager *phase.Manager, distroPath string, pkg *layout.DistroLayout, runErr error) {
		var cfg *cluster.Cluster
		if manager != nil {
			cfg = manager.Config
		}

		r := receipt.Build(cfg, sink.Result(), receipt.Options{
			Command:    command,
			Args:       args,
			Version:    config.CLIVersion,
			Commit:     config.CLICommit,
			ConfigPath: opt.config,
			Package:    packageProvenance(distroPath, pkg),
			StartedAt:  started,
			DryRun:     opt.dryRun,
			Observed:   sink.Observed(),
			Err:        runErr,
		})

		// The table goes to stdout, where a person reads it, rather than onto the log stream
		// beside it. It prints itself only when some phase reported per host.
		printHostSummary(OutputWriter, r)

		if path == "" {
			return
		}

		l := logger.From(ctx)
		if err := receipt.Write(path, r); err != nil {
			// Never allowed to mask the run's own error: the command still returns runErr,
			// and this is reported beside it. An operator who asked for a receipt and did not
			// get one needs to know, but not at the price of losing why the run failed.
			l.Error("unable to write the run receipt", "path", path, "error", err)
			return
		}
		l.Info("run receipt written", "path", path)
	}

	return ctx, finish, nil
}

// packageProvenance reads what a loaded package can prove about itself.
//
// distroPath is the package as the operator named it, which is what Path records and what the
// archive digest is taken over. The layout's own DirPath is deliberately not used for either: it
// is the temporary directory the package was extracted into, which is gone by the time anyone
// reads the receipt.
//
// The two digests answer different questions and both are worth keeping. SHA256 is the archive an
// operator was shipped, which is what they compare against a vendor's published checksum. Digest
// is the OCI manifest computed over the contents at load, which survives the package being
// repacked and is what matches a registry.
func packageProvenance(distroPath string, l *layout.DistroLayout) *receipt.Package {
	if l == nil {
		return nil
	}

	return &receipt.Package{
		Path:         distroPath,
		Name:         l.Distro.Metadata.Name,
		Version:      l.Distro.Metadata.Version,
		SHA256:       archiveDigest(distroPath),
		Digest:       l.Digest(),
		Architecture: string(l.Distro.Build.Architecture),
		Engine:       l.Distro.Spec.Type,
		Signed:       l.IsSigned(),
	}
}

// archiveDigest is the SHA-256 of the package as it was given, when it was given as a local file.
//
// A package named by an OCI reference has no archive to hash, and an absent value is the honest
// answer there rather than a digest of something else. Failure is silent for the same reason the
// receipt is written at all on the failure path: refusing to record anything because one optional
// field could not be computed would be the wrong trade.
func archiveDigest(path string) string {
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	sum, err := helpers.GetSHA256OfFile(path)
	if err != nil {
		return ""
	}
	return sum
}
