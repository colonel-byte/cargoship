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
	"regexp"

	"github.com/colonel-byte/zarf-distro/src/config"
	zconfig "github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/types"
)

var plainHTTP bool
var insecureSkipTLSVerify bool
var isCleanPathRegex = regexp.MustCompile(`^[a-zA-Z0-9\_\-\/\.\~\\:]+$`)

func defaultRemoteOptions() types.RemoteOptions {
	return types.RemoteOptions{
		PlainHTTP:             plainHTTP,
		InsecureSkipTLSVerify: insecureSkipTLSVerify,
	}
}

func setBaseDirectory(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "."
}

func getCachePath(ctx context.Context) (string, error) {
	if !isCleanPathRegex.MatchString(config.CommonOptions.CachePath) {
		logger.From(ctx).Warn("invalid characters in Zarf cache path, using default", "cfg", zconfig.ZarfDefaultCachePath, "default", zconfig.ZarfDefaultCachePath)
		config.CommonOptions.CachePath = zconfig.ZarfDefaultCachePath
	}
	return zconfig.GetAbsCachePath()
}
