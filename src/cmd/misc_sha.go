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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/pkg/helpers"
	"github.com/colonel-byte/cargoship/src/pkg/utils"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type sha256SumOptions struct {
	extractPath string
}

func newSha256SumCommand() *cobra.Command {
	o := sha256SumOptions{}

	cmd := &cobra.Command{
		Use:               "sha256sum [ FILE | URL ]",
		Args:              cobra.ExactArgs(1),
		Aliases:           []string{"sum"},
		Short:             lang.CmdSha256SumShort,
		RunE:              o.run,
		PersistentPreRunE: o.perprerun,
	}

	cmd.Flags().StringVarP(&o.extractPath, "extract-path", "e", "", lang.CmdSha256SumFlagExtractPath)

	return cmd
}

func (o *sha256SumOptions) perprerun(_ *cobra.Command, _ []string) error {
	return nil
}

func (o *sha256SumOptions) run(cmd *cobra.Command, args []string) (err error) {
	hashErr := errors.New("unable to compute the SHA256SUM hash")
	ctx := cmd.Context()

	fileName := args[0]

	var tmp string
	var data io.ReadCloser

	if helpers.IsURL(fileName) {
		logger.From(cmd.Context()).Warn("this is a remote source. If a published checksum is available you should use that rather than calculating it directly from the remote link")

		fileBase, err := helpers.ExtractBasePathFromURL(fileName)
		if err != nil {
			return errors.Join(hashErr, err)
		}

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
			Files: []string{extractedFile},
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
