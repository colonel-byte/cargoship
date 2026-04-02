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
	"fmt"
	"os"
	"strings"

	"github.com/colonel-byte/zarf-distro/src/config/lang"
	"github.com/spf13/viper"
	zarf "github.com/zarf-dev/zarf/src/cmd"
	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/zoci"
)

const (
	VPkgCreateOutput           = "distro.create.output"
	VPkgOCIConcurrency         = "distro.oci_concurrency"
	VPkgCreateRegistryOverride = "distro.create.registry_override"
)

var (
	v            *viper.Viper
	vConfigError error
)

func initViper() error {
	// Already initialized by some other command
	if v != nil {
		return nil
	}

	v = viper.New()
	cfgFile := os.Getenv("ZARF_DISTRO_CONFIG")

	// Don't forget to read config either from cfgFile or from home directory!
	if cfgFile != "" {
		// Use config file from the flag.
		v.SetConfigFile(cfgFile)
	} else {
		// Search config paths (order matters!)
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.zarf")
		v.SetConfigName("zarf-distro-config")
		v.SetConfigType("yaml")
		v.SetConfigType("yml")
	}

	v.SetEnvPrefix("distro")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	setDefaults()

	log, err := logger.New(logger.ConfigDefault())
	if err != nil {
		return fmt.Errorf("failed to create logger: %v", err)
	}

	vConfigError = v.ReadInConfig()
	if vConfigError != nil {
		// Config file not found; ignore
		if _, ok := vConfigError.(viper.ConfigFileNotFoundError); !ok {
			log.Warn(lang.CmdViperErrLoadingConfigFile, "error", vConfigError.Error())
		}
	}
	return nil
}

func setDefaults() {
	v.SetDefault(zarf.VLogLevel, "info")
	v.SetDefault(zarf.VZarfCache, config.ZarfDefaultCachePath)
	v.SetDefault(zarf.VLogFormat, string(logger.FormatConsole))

	v.SetDefault(VPkgOCIConcurrency, zoci.DefaultConcurrency)
}
