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

package phase

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/zarf-distro/src/config"
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// UploadFiles implements a phase which upload files to hosts
type UploadFiles struct {
	GenericPhase

	hosts    cluster.ZarfHosts
	disFiles []distro.ZarfFiles
}

// Title for the phase
func (p *UploadFiles) Title() string {
	return "Upload files to hosts"
}

// Prepare the phase
func (p *UploadFiles) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.manager.Config = c
	p.hosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return (len(h.Files) + len(d.Spec.Config.Files)) > 0
	})
	p.disFiles = p.manager.Distro.Spec.Config.Files

	for _, i := range p.manager.Distro.Spec.Config.Config.Images {
		logger.From(ctx).Warn(i)
	}

	return nil
}

// ShouldRun is true when there are workers
func (p *UploadFiles) ShouldRun() bool {
	return len(p.hosts) > 0
}

// Run the phase
func (p *UploadFiles) Run(ctx context.Context) error {
	return p.parallelDoUpload(ctx, p.manager.Config.Spec.Hosts, p.uploadFiles)
}

func (p *UploadFiles) uploadFiles(ctx context.Context, h *cluster.ZarfHost) error {
	files := []cluster.UploadFile{}

	for i, f := range p.disFiles {
		if ctx.Err() != nil {
			return fmt.Errorf("upload canceled: %w", ctx.Err())
		}
		logger.From(ctx).Debug("need to upload from distro package", "source", filepath.Join(p.manager.TempDirectory, config.FilesDir, strconv.Itoa(i), filepath.Base(f.Target)), "target", f.Target)
		files = append(files, cluster.UploadFile{
			Name:            filepath.Base(f.Target),
			DestinationFile: f.Target,
			Sources: []*cluster.LocalFile{
				{
					Path: filepath.Join(p.manager.TempDirectory, config.FilesDir, strconv.Itoa(i), filepath.Base(f.Target)),
				},
			},
		})
	}
	for _, f := range h.Files {
		if ctx.Err() != nil {
			return fmt.Errorf("upload canceled: %w", ctx.Err())
		}
		logger.From(ctx).Debug("need to upload", "target", f.Destination)
		if f.Data != "" {
			p.uploadData(ctx, h, &cluster.UploadFile{
				Name:            filepath.Base(f.Destination),
				DestinationFile: f.Destination,
				Data:            f.Data,
			})
		}
	}
	logger.From(ctx).Warn("total files", "count", len(files))
	for _, f := range files {
		p.uploadFile(ctx, h, &f)
	}
	return nil
}

func (p *UploadFiles) ensureDir(ctx context.Context, h *cluster.ZarfHost, dir, perm, owner string) error {
	logger.From(ctx).Debug("ensuring directory exists", "host", h, "dir", dir)
	if !h.Configurer.FileExist(h, dir) {
		targetPerm := perm
		if targetPerm == "" {
			targetPerm = "0755"
		}
		err := p.Wet(h, fmt.Sprintf("create a directory for uploading: `mkdir -p \"%s\"`", dir), func() error {
			if v, perr := strconv.ParseUint(targetPerm, 8, 32); perr == nil {
				return h.SudoFsys().MkDirAll(dir, fs.FileMode(v))
			}
			return h.Configurer.MkDir(h, dir, exec.Sudo(h))
		})
		if err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	if owner != "" {
		err := p.Wet(h, fmt.Sprintf("set owner for directory %s to %s", dir, owner), func() error {
			return h.Configurer.Chown(h, dir, owner, exec.Sudo(h))
		})
		if err != nil {
			return err
		}
	}

	if perm == "" {
		perm = "0755"
	}

	return p.Wet(h, fmt.Sprintf("set permissions for directory %s to %s", dir, perm), func() error {
		return chmodWithString(h, dir, perm)
	})
}

func (p *UploadFiles) uploadFile(ctx context.Context, h *cluster.ZarfHost, f *cluster.UploadFile) error {
	logger.From(ctx).Info("uploading", "host", h, "file", f)
	numfiles := len(f.Sources)

	for i, s := range f.Sources {
		dest := f.DestinationFile
		if dest == "" {
			dest = path.Join(f.DestinationDir, s.Path)
		}

		src := path.Join(f.Base, s.Path)
		if numfiles > 1 {
			logger.From(ctx).Info("uploading file", "host", h, "source", src, "destination", dest, "count", numfiles, "current", i+1)
		}

		owner := f.Owner()

		if err := p.ensureDir(ctx, h, path.Dir(dest), f.DirPermString, owner); err != nil {
			return err
		}

		var stat os.FileInfo
		var err error
		if h.FileChanged(src, dest) {
			stat, err = os.Stat(src)
			if err != nil {
				return fmt.Errorf("failed to stat local file %s: %w", src, err)
			}
			err := p.Wet(h, fmt.Sprintf("upload file %s => %s", src, dest), func() error {
				stat, err := os.Stat(src)
				if err != nil {
					return fmt.Errorf("failed to stat local file %s: %w", src, err)
				}
				perm := stat.Mode()
				if s.PermMode != "" {
					if v, perr := strconv.ParseUint(s.PermMode, 8, 32); perr == nil {
						perm = fs.FileMode(v)
					}
				}
				return h.Upload(path.Join(f.Base, s.Path), dest, perm, exec.Sudo(h), exec.LogError(true))
			})
			if err != nil {
				return err
			}
		} else {
			logger.From(ctx).Info("file already exists and hasn't been changed, skipping upload", "host", h)
		}

		if stat == nil {
			stat, err = os.Stat(src)
			if err != nil {
				return fmt.Errorf("failed to stat %s: %w", src, err)
			}
		}
		modTime := stat.ModTime()
		if err := p.applyFileMetadata(ctx, h, dest, owner, s.PermMode, &modTime); err != nil {
			return err
		}
	}

	return nil
}

func (p *UploadFiles) uploadData(ctx context.Context, h *cluster.ZarfHost, f *cluster.UploadFile) error {
	logger.From(ctx).Info("uploading inline data", "host", h)
	dest := f.DestinationFile
	if dest == "" {
		if f.DestinationDir != "" {
			dest = path.Join(f.DestinationDir, f.Name)
		} else {
			dest = f.Name
		}
	}

	owner := f.Owner()

	if err := p.ensureDir(ctx, h, path.Dir(dest), f.DirPermString, owner); err != nil {
		return err
	}

	err := p.Wet(h, fmt.Sprintf("upload inline data => %s", dest), func() error {
		fileMode, _ := strconv.ParseUint(f.PermString, 8, 32)
		remoteFile, err := h.SudoFsys().OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(fileMode))
		if err != nil {
			return err
		}

		defer func() {
			if err := remoteFile.Close(); err != nil {
				logger.From(ctx).Warn("failed to close remote file", "host", h, "destination", dest, "error", err)
			}
		}()

		_, err = fmt.Fprint(remoteFile, f.Data)

		return err
	})
	if err != nil {
		return err
	}

	return p.applyFileMetadata(ctx, h, dest, owner, "", nil)
}

func (p *UploadFiles) applyFileMetadata(ctx context.Context, h *cluster.ZarfHost, dest, owner, perm string, timestamp *time.Time) error {
	if owner != "" {
		err := p.Wet(h, fmt.Sprintf("set owner for %s to %s", dest, owner), func() error {
			logger.From(ctx).Debug("setting owner", "host", h, "owner", owner, "destination", dest)
			return h.Configurer.Chown(h, dest, owner, exec.Sudo(h))
		})
		if err != nil {
			return err
		}
	}

	if perm != "" {
		err := p.Wet(h, fmt.Sprintf("set permissions for %s to %s", dest, perm), func() error {
			logger.From(ctx).Debug("setting permissions", "host", h, "permission", perm, "destination", dest)
			return chmodWithString(h, dest, perm)
		})
		if err != nil {
			return err
		}
	}

	if timestamp != nil {
		err := p.Wet(h, fmt.Sprintf("set timestamp for %s to %s", dest, timestamp.String()), func() error {
			logger.From(ctx).Debug("setting touching", "host", h, "destination", dest)
			return h.Configurer.Touch(h, dest, *timestamp, exec.Sudo(h))
		})
		if err != nil {
			return fmt.Errorf("failed to touch %s: %w", dest, err)
		}
	}
	return nil
}

func chmodWithString(h *cluster.ZarfHost, path, perm string) error {
	mode, err := strconv.ParseUint(perm, 8, 32)
	if err != nil {
		return fmt.Errorf("invalid file mode %q: %w", perm, err)
	}
	return chmodWithMode(h, path, fs.FileMode(mode))
}

func chmodWithMode(h *cluster.ZarfHost, path string, mode fs.FileMode) error {
	perm := fmt.Sprintf("%04o", uint32(mode)&0o7777)
	return h.Configurer.Chmod(h, path, perm, exec.Sudo(h))
}
