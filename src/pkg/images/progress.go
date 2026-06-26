// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
//
// Modifications Copyright 2026 colonel-byte.
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

package images

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zarf-dev/zarf/src/pkg/utils"
	"oras.land/oras-go/v2"
)

// Report defines a function to log progress
type Report func(bytesRead, totalBytes int64)

// DefaultReport returns a default report function
func DefaultReport(l *slog.Logger, msg string, imageName string) Report {
	return func(bytesRead, totalBytes int64) {
		if totalBytes <= 0 {
			l.Warn("total bytes is a non-positive integer, can't report progress")
			return
		}
		percentComplete := float64(bytesRead) / float64(totalBytes) * 100
		remaining := float64(totalBytes) - float64(bytesRead)
		l.Info(msg, "name", imageName, "complete", fmt.Sprintf("%.1f%%", percentComplete), "remaining", utils.ByteFormat(remaining, 2))
	}
}

const defaultProgressInterval = 10 * time.Second

// StartReporting starts the reporting goroutine
func (tt *Tracker) StartReporting(ctx context.Context) {
	tt.wg.Add(1)
	go func() {
		defer tt.wg.Done()
		ticker := time.NewTicker(tt.reportInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				tt.reporter(tt.bytesRead.Load(), tt.totalBytes)
			case <-tt.stopReports:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// StopReporting stops the reporting goroutine.
func (tt *Tracker) StopReporting() {
	tt.stopOnce.Do(func() {
		if tt.stopReports != nil {
			close(tt.stopReports)
		}
	})
	tt.wg.Wait()
}

// Tracker reports progress against totalBytes as bytesRead gets updated
type Tracker struct {
	reporter       Report
	reportInterval time.Duration
	bytesRead      *atomic.Int64
	totalBytes     int64

	stopReports chan struct{}
	stopOnce    sync.Once
	wg          sync.WaitGroup
}

// TrackedTarget wraps an oras.Target to track progress
type TrackedTarget struct {
	oras.Target
	*Tracker
}

// NewTrackedTarget creates a new TrackedTarget
func NewTrackedTarget(target oras.Target, totalBytes int64, reporter Report) *TrackedTarget {
	tracker := &Tracker{
		reporter:       reporter,
		reportInterval: defaultProgressInterval,
		bytesRead:      &atomic.Int64{},
		totalBytes:     totalBytes,
		stopReports:    make(chan struct{}),
	}
	return &TrackedTarget{
		Target:  target,
		Tracker: tracker,
	}
}
