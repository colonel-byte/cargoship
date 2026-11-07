// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
//
// Modifications Copyright 2026 colonel-byte.
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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/cmd/flags"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/pkg/coci"
	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/colonel-byte/cargoship/pkg/utils"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type sha256SumOptions struct {
	extractPath string
	layer       string
	arch        string
}

func newSha256SumCommand() *cobra.Command {
	o := sha256SumOptions{}

	cmd := &cobra.Command{
		Use:     "sha256sum [ FILE | URL | OCI_REF ]",
		Args:    cobra.ExactArgs(1),
		Aliases: []string{"sum"},
		Short:   lang.CmdSha256SumShort,
		Example: lang.CmdSha256SumExample,
		RunE:    o.run,
	}

	cmd.Flags().StringVarP(&o.extractPath, "extract-path", "e", "", lang.CmdSha256SumFlagExtractPath)
	cmd.Flags().StringVar(&o.layer, "layer", "", lang.CmdSha256SumFlagLayer)
	cmd.Flags().StringVar(&o.arch, "arch", "", lang.CmdSha256SumFlagArch)

	addTempDirFlags(cmd)
	// An oci:// argument reaches a registry, so this command takes the transport flags the
	// other registry-reading commands take.
	addRegistryFlags(cmd)

	if err := cmd.RegisterFlagCompletionFunc("arch", o.completeArch); err != nil {
		fmt.Fprintf(os.Stderr, "failed to register arch flag completion: %v\n", err)
	}
	if err := cmd.RegisterFlagCompletionFunc("layer", o.completeLayer); err != nil {
		fmt.Fprintf(os.Stderr, "failed to register layer flag completion: %v\n", err)
	}
	if err := cmd.RegisterFlagCompletionFunc("extract-path", o.completeExtractPath); err != nil {
		fmt.Fprintf(os.Stderr, "failed to register extract-path flag completion: %v\n", err)
	}

	return cmd
}

// completionTimeout bounds the registry read a tab completion does. A completion that hangs is
// worse than one that falls back to the static list, so the reference is given a short budget
// and the generic suggestions answer if it is not met.
const completionTimeout = 3 * time.Second

// completeArch suggests the architectures --arch can take.
//
// When the reference being checksummed is an oci:// one, the suggestions are the architectures
// that reference actually offers, read from its index. That is the useful answer: the flag is
// required for an index and the set is a property of the artifact, not of cargoship -- a zarf
// package offers "skeleton" alongside amd64 and arm64, and ghcr.io/cri-o/bundle offers ppc64le
// and s390x, none of which the static list knows about.
//
// Anything that stops that from working -- a local path, an unreachable registry, credentials
// cargoship does not have, a reference resolving to a single manifest -- falls back to the
// architectures cargoship itself builds for. A completion must always answer something, and it
// must never be the place an operator first learns a registry is down.
func (o *sha256SumOptions) completeArch(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 || !helpers.IsOCIURL(args[0]) {
		return flags.RegisterArchitectureFormat(cmd, args, toComplete)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), completionTimeout)
	defer cancel()

	platforms, err := coci.FileArches(ctx, args[0], coci.FileOptions{
		RemoteOptions: defaultRemoteOptions(),
		CachePath:     config.CommonOptions.CachePath,
	})
	if err != nil || len(platforms) == 0 {
		return flags.RegisterArchitectureFormat(cmd, args, toComplete)
	}

	suggestions := archSuggestions(platforms, toComplete)
	if len(suggestions) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return suggestions, cobra.ShellCompDirectiveNoFileComp
}

// completeLayer suggests the layer titles --layer can take, read from the artifact.
//
// Unlike --arch there is no generic fallback: a layer title is entirely a property of the
// artifact, so a reference that cannot be read has nothing to suggest and says nothing.
//
// When no --arch has been given and the reference is an index over several, the titles come
// from one of its children rather than from nothing, and an active-help line says which and
// recommends giving --arch. Every artifact this has been used against titles its layers
// identically across platforms, so the suggestions are right in practice -- and a title that
// turns out not to exist for the architecture finally requested fails at fetch time with the
// full list, which is the loud failure that makes the guess safe to offer.
func (o *sha256SumOptions) completeLayer(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 || !helpers.IsOCIURL(args[0]) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), completionTimeout)
	defer cancel()

	listing, err := coci.FileLayers(ctx, args[0], o.arch, coci.FileOptions{
		RemoteOptions: defaultRemoteOptions(),
		CachePath:     config.CommonOptions.CachePath,
	})
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	suggestions := layerSuggestions(listing, toComplete)
	if listing.Assumed {
		// Said twice, through two channels, because neither reaches every shell. ActiveHelp
		// is the idiomatic one and renders as a single non-selectable line -- but cobra's
		// generated fish script disables it outright, so fish would show nothing at all. The
		// per-suggestion description in layerSuggestions is what fish does render.
		suggestions = cobra.AppendActiveHelp(suggestions,
			fmt.Sprintf("these titles are from the %s manifest; pass --arch for consistent results", listing.Arch))
	}
	return suggestions, cobra.ShellCompDirectiveNoFileComp
}

// layerSuggestions renders an artifact's layer titles as cobra completion entries, keeping
// those matching what has been typed.
//
// A listing that had to assume an architecture carries that on every entry, as the description
// shells show beside the value. It is repetitive next to the one-line ActiveHelp summary, and
// it is the only form a fish user ever sees.
func layerSuggestions(listing coci.LayerListing, toComplete string) []string {
	description := ""
	if listing.Assumed {
		description = fmt.Sprintf("\tfrom the %s manifest; pass --arch", listing.Arch)
	}
	suggestions := make([]string, 0, len(listing.Titles))
	for _, title := range listing.Titles {
		if strings.HasPrefix(title, toComplete) {
			suggestions = append(suggestions, title+description)
		}
	}
	return suggestions
}

// completeExtractPath suggests the paths inside the selected layer.
//
// This one is deliberately the most reluctant of the three. Suggesting members means reading
// the layer itself rather than a manifest -- tens of mebibytes for a vendor engine bundle --
// so it only does so once both --layer and --arch have been given and the bytes to read are
// therefore not in question. On an artifact published for several architectures, an assumed
// platform would mean downloading a layer the operator never asked for.
//
// What it does read is exactly the blob the command is about to fetch anyway, into the same
// cache, so following the completion through costs nothing extra and a second tab is served
// locally. For a local or http source the extraction is of a file cargoship has not got, so
// there is nothing to list and nothing is offered.
func (o *sha256SumOptions) completeExtractPath(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 || !helpers.IsOCIURL(args[0]) || o.layer == "" || o.arch == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), completionTimeout)
	defer cancel()

	members, err := coci.FileMembers(ctx, args[0], coci.FileSelector{
		Arch:  o.arch,
		Layer: o.layer,
	}, coci.FileOptions{
		RemoteOptions: defaultRemoteOptions(),
		CachePath:     config.CommonOptions.CachePath,
	})
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	suggestions := make([]string, 0, len(members))
	for _, member := range members {
		if strings.HasPrefix(member, toComplete) {
			suggestions = append(suggestions, member)
		}
	}
	return suggestions, cobra.ShellCompDirectiveNoFileComp
}

// archSuggestions renders the architectures an artifact offers as cobra completion entries,
// keeping those matching what has been typed.
//
// The description is the platform as the artifact declares it, so an operator can see that a
// zarf package says multi/amd64 where a vendor bundle says linux/amd64 -- the same difference
// that makes a single hardcoded OS unable to read both.
func archSuggestions(platforms []coci.ArtifactPlatform, toComplete string) []string {
	suggestions := make([]string, 0, len(platforms))
	for _, p := range platforms {
		if !strings.HasPrefix(p.Arch, toComplete) {
			continue
		}
		suggestions = append(suggestions, fmt.Sprintf("%s\t%s/%s", p.Arch, p.OS, p.Arch))
	}
	return suggestions
}

func (o *sha256SumOptions) run(cmd *cobra.Command, args []string) (err error) {
	hashErr := errors.New("unable to compute the SHA256SUM hash")
	ctx := cmd.Context()
	l := logger.From(ctx)

	fileName := args[0]

	var tmp string
	var data io.ReadCloser

	if helpers.IsOCIURL(fileName) {
		l.Warn("this is a remote source. If a published checksum is available you should use that rather than calculating it directly from the registry. A reference pinned by digest -- name@sha256:... -- cannot be re-tagged underneath you")

		tmp, err = utils.MakeTempDir(config.CommonOptions.TempDirectory)
		if err != nil {
			return errors.Join(hashErr, err)
		}
		defer func(path string) {
			errRemove := os.RemoveAll(path)
			err = errors.Join(err, errRemove)
		}(tmp)

		// --arch is deliberately not defaulted to this host's architecture. The digest being
		// computed is going into a file entry that names its own selector.arch, and an
		// operator on an arm64 workstation filling in an amd64 entry would otherwise get a
		// plausible wrong answer with nothing to notice. An artifact published as a single
		// manifest needs no architecture and is unaffected; one published as a real index
		// says so and names the flag.
		l.Debug("resolving OCI reference", "reference", fileName, "arch", o.arch, "layer", o.layer)

		fetched := filepath.Join(tmp, "oci-file")
		fetchErr := coci.FetchFile(ctx, fileName, coci.FileSelector{
			Arch:        o.arch,
			Layer:       o.layer,
			ExtractPath: o.extractPath,
		}, fetched, coci.FileOptions{
			RemoteOptions: defaultRemoteOptions(),
			CachePath:     config.CommonOptions.CachePath,
		})
		switch {
		case errors.Is(fetchErr, coci.ErrArchRequired):
			return errors.Join(hashErr, fmt.Errorf("%w: give one with --arch", fetchErr))
		case errors.Is(fetchErr, coci.ErrLayerRequired):
			return errors.Join(hashErr, fmt.Errorf("%w: name it with --layer", fetchErr))
		case fetchErr != nil:
			return errors.Join(hashErr, fetchErr)
		}

		// coci has already applied the extract path, so the shared extraction below must not
		// run again over a file that is no longer an archive.
		o.extractPath = ""
		fileName = fetched
	} else if helpers.IsURL(fileName) {
		l.Warn("this is a remote source. If a published checksum is available you should use that rather than calculating it directly from the remote link")

		fileBase, err := helpers.ExtractBasePathFromURL(fileName)
		if err != nil {
			return errors.Join(hashErr, err)
		}

		l.Debug("base", "fileBase", fileBase)

		if fileBase == "" {
			fileBase = "sha-file"
		}

		tmp, err = utils.MakeTempDir(config.CommonOptions.TempDirectory)
		if err != nil {
			return errors.Join(hashErr, err)
		}

		downloadPath := filepath.Join(tmp, fileBase)
		err = utils.DownloadToFile(ctx, fileName, downloadPath)
		if err != nil {
			return errors.Join(hashErr, err)
		}

		fileName = downloadPath

		defer func(path string) {
			errRemove := os.RemoveAll(path)
			err = errors.Join(err, errRemove)
		}(tmp)
	}

	if o.extractPath != "" {
		if tmp == "" {
			tmp, err = utils.MakeTempDir(config.CommonOptions.TempDirectory)
			if err != nil {
				return errors.Join(hashErr, err)
			}
			defer func(path string) {
				errRemove := os.RemoveAll(path)
				err = errors.Join(err, errRemove)
			}(tmp)
		}

		extractedFile := filepath.Join(tmp, o.extractPath)

		decompressOpts := archive.DecompressOpts{
			// We want to extract the file relative to the tar ball
			Files: []string{o.extractPath},
		}
		err = archive.Decompress(ctx, fileName, tmp, decompressOpts)
		if err != nil {
			return errors.Join(hashErr, err)
		}

		fileName = extractedFile
	}

	data, err = os.Open(fileName)
	if err != nil {
		return errors.Join(hashErr, err)
	}
	defer func(data io.ReadCloser) {
		errClose := data.Close()
		err = errors.Join(err, errClose)
	}(data)

	hash, err := helpers.GetSHA256Hash(data)
	if err != nil {
		return errors.Join(hashErr, err)
	}
	fmt.Println(hash)
	return nil
}
