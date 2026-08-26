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

// Package riglogger add settings that overrides the rig.Logger
package riglogger

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/k0sproject/rig/log"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type rigLogger struct {
	log.Logger
	ctx context.Context
}

// Debugf implements log.Logger.
func (l rigLogger) Debugf(msg string, args ...any) {
	logger.From(l.ctx).Debug(fmt.Sprintf(msg, args...), argAttrs(args))
}

// Errorf implements log.Logger.
func (l rigLogger) Errorf(msg string, args ...any) {
	logger.From(l.ctx).Error(fmt.Sprintf(msg, args...), argAttrs(args))
}

// Infof implements log.Logger.
func (l rigLogger) Infof(msg string, args ...any) {
	logger.From(l.ctx).Info(fmt.Sprintf(msg, args...), argAttrs(args))
}

// Tracef implements log.Logger.
func (l rigLogger) Tracef(msg string, args ...any) {
	logger.From(l.ctx).Debug(fmt.Sprintf(msg, args...), argAttrs(args))
}

// Warnf implements log.Logger.
func (l rigLogger) Warnf(msg string, args ...any) {
	logger.From(l.ctx).Warn(fmt.Sprintf(msg, args...), argAttrs(args))
}

// argAttrs turns rig's positional printf args into an "args" group of slog key-value pairs so
// structured sinks like the JSON file handler retain each raw value instead of only the single
// flattened message string that fmt.Sprintf produces. When args is empty, slog.Group returns a
// zero Attr that handlers skip, so no empty "args" object is logged.
func argAttrs(args []any) slog.Attr {
	pairs := make([]any, 0, len(args)*2)
	for i, arg := range args {
		pairs = append(pairs, fmt.Sprint(args[i]), arg)
	}
	return slog.Group("args", pairs...)
}

// RigLogger overrides the rig.Log with our custom logger
func RigLogger(ctx context.Context) error {
	log.Log = rigLogger{
		ctx: ctx,
	}
	return nil
}
