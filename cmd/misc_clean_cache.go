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
	"time"

	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/pkg/utils"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type cleanCacheOptions struct {
	olderThan time.Duration
	dryRun    bool
}

func newCleanCacheCommand() *cobra.Command {
	o := cleanCacheOptions{}
	cmd := &cobra.Command{
		Use:     "clean-cache",
		Args:    cobra.NoArgs,
		Short:   lang.CmdCleanCacheShort,
		Long:    lang.CmdCleanCacheLong,
		Example: lang.CmdCleanCacheExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return o.run(cmd.Context())
		},
	}

	cmd.Flags().DurationVar(&o.olderThan, "older-than", 0, lang.CmdCleanCacheFlagOlderThan)
	cmd.Flags().BoolVar(&o.dryRun, InstallDryRun, false, lang.CmdCleanCacheFlagDryRun)

	addBuildFlags(cmd)

	return cmd
}

func (o *cleanCacheOptions) run(ctx context.Context) error {
	l := logger.From(ctx)

	cachePath, err := getCachePath(ctx)
	if err != nil {
		return err
	}

	// Reported before anything is removed, and under --dry-run it is the whole output: an
	// operator deciding whether to spend a refetch wants to see what is there first.
	areas, err := utils.CacheUsage(cachePath)
	if err != nil {
		return err
	}
	if len(areas) == 0 {
		l.Info("nothing cached", "cache", cachePath)
		return nil
	}

	var total int64
	for _, area := range areas {
		total += area.Size
		l.Info("cached",
			"area", area.Name,
			"size", utils.HumanBytes(area.Size),
			"entries", area.Entries,
			"prunable", area.Kind != utils.CacheAreaRecord)
	}
	l.Info("cache total", "cache", cachePath, "size", utils.HumanBytes(total))

	pruned, err := utils.PruneCache(ctx, cachePath, utils.PruneOptions{
		OlderThan: o.olderThan,
		DryRun:    o.dryRun,
	})
	if err != nil {
		return err
	}

	for _, name := range pruned.SkippedLayouts {
		l.Warn("left an image layout alone: it is an index over shared blobs, so it cannot be pruned by age without breaking it; rerun without --older-than to remove it whole",
			"area", name)
	}

	if pruned.Entries == 0 {
		l.Info("nothing to remove", "olderThan", o.olderThan)
		return nil
	}

	verb := "removed"
	if o.dryRun {
		verb = "would remove"
	}

	for _, area := range pruned.Dirs {
		l.Info(verb, "area", area.Name, "size", utils.HumanBytes(area.Size), "entries", area.Entries)
	}

	l.Info(fmt.Sprintf("%s cached content", verb),
		"size", utils.HumanBytes(pruned.Size),
		"entries", pruned.Entries)

	return nil
}
