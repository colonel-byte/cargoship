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
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

func (p *GenericPhase) ensureDir(ctx context.Context, h *cluster.ZarfHost, dir, perm, owner string) error {
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

func (p *GenericPhase) uploadFiles(ctx context.Context, h *cluster.ZarfHost, files []v1alpha1.ZarfFile) error {
	for i, f := range files {
		logger.From(ctx).Debug("file", "num", i+1, "count", len(files))
		p.uploadFile(ctx, h, &f)
	}

	return nil
}

func (p *GenericPhase) uploadFile(ctx context.Context, h *cluster.ZarfHost, f *v1alpha1.ZarfFile) error {
	logger.From(ctx).Info("uploading", "host", h, "file", f)
	owner := f.Owner()

	if err := p.ensureDir(ctx, h, path.Dir(f.Target), f.DirPermString, owner); err != nil {
		return err
	}
	if f.TargetIsDir {
		if err := p.ensureDir(ctx, h, f.Target, f.DirPermString, owner); err != nil {
			return err
		}
	}
	src := path.Join(f.Base, f.LocalSource.Path)
	var stat os.FileInfo
	var err error

	target := f.Target
	if f.TargetIsDir {
		target = filepath.Join(f.Target, filepath.Base(f.LocalSource.Path))
	}

	if h.FileChanged(src, target) {
		stat, err = os.Stat(src)
		if err != nil {
			return fmt.Errorf("failed to stat local file %s: %w", src, err)
		}
		err := p.Wet(h, fmt.Sprintf("upload file %s => %s", src, target), func() error {
			stat, err := os.Stat(src)
			if err != nil {
				return fmt.Errorf("failed to stat local file %s: %w", src, err)
			}
			perm := stat.Mode()
			if f.LocalSource.PermMode != "" {
				if v, perr := strconv.ParseUint(f.LocalSource.PermMode, 8, 32); perr == nil {
					perm = fs.FileMode(v)
				}
			}
			err = h.Upload(path.Join(f.Base, f.LocalSource.Path), target, perm, exec.Sudo(h), exec.LogError(true))
			if err != nil {
				return err
			}
			return h.Touch(target, time.Unix(0, 0), exec.Sudo(h))
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

	modTime := time.Unix(0, 0)
	if err := p.applyFileMetadata(ctx, h, f.Target, owner, f.LocalSource.PermMode, &modTime); err != nil {
		return err
	}

	return nil
}

func (p *GenericPhase) uploadData(ctx context.Context, h *cluster.ZarfHost, f *v1alpha1.ZarfFile) error {
	logger.From(ctx).Info("uploading inline data", "host", h)
	dest := f.Target

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

func (p *GenericPhase) applyFileMetadata(ctx context.Context, h *cluster.ZarfHost, dest, owner, perm string, timestamp *time.Time) error {
	if owner != "" {
		logger.From(ctx).Debug("setting owner", "host", h, "owner", owner, "destination", dest)
		err := h.Configurer.Chown(h, dest, owner, exec.Sudo(h))
		if err != nil {
			return err
		}
	}

	if perm != "" {
		logger.From(ctx).Debug("setting permissions", "host", h, "permission", perm, "destination", dest)
		err := chmodWithString(h, dest, perm)
		if err != nil {
			return err
		}
	}

	if timestamp != nil {
		logger.From(ctx).Debug("setting touching", "host", h, "destination", dest)
		err := h.Configurer.Touch(h, dest, *timestamp, exec.Sudo(h))
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

// stageTempPath returns a temp file path for the engine files on the host,
// preserving the .exe extension on Windows so the file remains executable.
func stageTempPath(isWin bool, bin string) string {
	ts := strconv.FormatInt(time.Now().UnixNano(), 10)
	if isWin {
		if ext := filepath.Ext(bin); strings.EqualFold(ext, ".exe") {
			return strings.TrimSuffix(bin, ext) + ".tmp." + ts + ext
		}
	}
	return bin + ".tmp." + ts
}
