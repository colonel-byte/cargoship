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
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

var serviceLogFilenameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9.-]+`)

// captureServiceLogsOnFailure is a no-op when waitErr is nil. Otherwise it best-effort fetches
// h's systemd journal for service, covering the window since startedAt, and writes it to a
// local file -- so a service that never became ready leaves behind more to debug than just a
// timeout error. It always returns waitErr unchanged: a failure to capture logs must never mask
// or replace the real error that made the wait fail.
func (p *GenericPhase) captureServiceLogsOnFailure(ctx context.Context, h *cluster.ZarfHost, service string, startedAt time.Time, waitErr error) error {
	if waitErr == nil {
		return nil
	}

	l := logger.From(ctx)

	// "@<unix-seconds>" is journalctl's locale/timezone-independent form for --since.
	cmd := fmt.Sprintf("journalctl -u %s --no-pager --since=@%d", service, startedAt.Unix())
	output, err := h.Sudo().ExecOutput(cmd)
	if err != nil {
		l.Warn("failed to collect remote service logs for debugging", "host", h, "service", service, "error", err)
		return waitErr
	}

	path, err := writeServiceLogFile(h, service, output)
	if err != nil {
		l.Warn("failed to save remote service logs for debugging", "host", h, "service", service, "error", err)
		return waitErr
	}

	l.Warn("service did not become ready in time, saved its logs for debugging", "host", h, "service", service, "path", path)
	return waitErr
}

// writeServiceLogFile saves content under the same cache/logs directory cargoship's own
// --log-file debug log uses, named so repeated failures for the same host/service don't
// clobber each other.
func writeServiceLogFile(h *cluster.ZarfHost, service, content string) (string, error) {
	cacheDir, err := config.GetAbsCachePath()
	if err != nil || cacheDir == "" {
		cacheDir = config.DefaultCachePath
	}
	logsDir := filepath.Join(cacheDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create log directory: %w", err)
	}

	name := fmt.Sprintf(
		"%s-%s-%s.log",
		serviceLogFilenameSanitizer.ReplaceAllString(h.String(), "_"),
		serviceLogFilenameSanitizer.ReplaceAllString(service, "_"),
		time.Now().Format(config.TimeFormat),
	)
	path := filepath.Join(logsDir, name)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write log file: %w", err)
	}
	return path, nil
}
