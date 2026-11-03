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
	"encoding/json"
	"fmt"

	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/internal/receipt"
	"github.com/spf13/cobra"
)

// MiscRedactReceiptOutput flag
const MiscRedactReceiptOutput = "output"

type redactReceiptOptions struct {
	output string
}

func newRedactReceiptCommand() *cobra.Command {
	o := redactReceiptOptions{}

	cmd := &cobra.Command{
		Use:     "redact-receipt [RECEIPT]",
		Args:    cobra.ExactArgs(1),
		Short:   lang.CmdRedactReceiptShort,
		Long:    lang.CmdRedactReceiptLong,
		Example: lang.CmdRedactReceiptExample,
		RunE:    o.run,
	}

	cmd.Flags().StringVarP(&o.output, MiscRedactReceiptOutput, "o", "", lang.CmdRedactReceiptFlagOutput)
	if err := cmd.MarkFlagFilename(MiscRedactReceiptOutput, "json"); err != nil {
		return cmd
	}

	return cmd
}

func (o *redactReceiptOptions) run(cmd *cobra.Command, args []string) error {
	in, err := receipt.Read(args[0])
	if err != nil {
		return err
	}

	// Re-redacting is refused rather than quietly repeated. It is almost always a sign that the
	// wrong file was reached for, and the one thing a second pass cannot do is put back what the
	// first one dropped -- so saying so is more useful than succeeding.
	if in.Redacted {
		return fmt.Errorf("%s is already redacted", args[0])
	}

	out := receipt.Redact(in)

	// Written with the same restrictive mode as a full receipt. A redacted receipt is safe to
	// share deliberately, which is not the same as safe to leave world-readable on a shared
	// management host.
	if o.output != "" {
		if err := receipt.Write(o.output, out); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n", o.output)
		return nil
	}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("unable to encode the redacted receipt: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s\n", b)
	return nil
}
