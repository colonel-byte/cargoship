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
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/types"
	"github.com/spf13/viper"
	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
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
	cfgFile := os.Getenv("CARGOSHIP_CONFIG")

	// Don't forget to read config either from cfgFile or from home directory!
	if cfgFile != "" {
		// Use config file from the flag.
		v.SetConfigFile(cfgFile)
	} else {
		// Search config paths (order matters!)
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.zarf")
		v.SetConfigName("cargoship-config")
		v.SetConfigType("yaml")
		v.SetConfigType("yml")
	}

	v.SetEnvPrefix("distro")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	setDefaults()

	log, err := logger.New(logger.Config{
		Level:       logger.Info,
		Format:      logger.FormatConsole,
		Destination: os.Stdout,
		Color:       true,
	})
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	vConfigError = v.ReadInConfig()
	if vConfigError != nil {
		// Config file not found; ignore
		if pathError, ok := errors.AsType[*viper.ConfigFileNotFoundError](vConfigError); ok {
			log.Warn(lang.CmdViperErrLoadingConfigFile, "error", pathError)
		}
	}
	return nil
}

func setDefaults() {
	v.SetDefault(types.LogLevel, types.LoggingLevelDefault)
	v.SetDefault(types.ZarfCache, config.ZarfDefaultCachePath)
	v.SetDefault(types.LogFormat, string(logger.FormatConsole))
	v.SetDefault(types.TmpDir, "/tmp")
	v.SetDefault(types.NoColor, false)

	v.SetDefault(types.DistroOCIConcurrency, 6)
	v.SetDefault(types.DistroCreateSkipSbom, false)
	v.SetDefault(types.DistroConcurrency, 30)
	v.SetDefault(types.DistroUpdateHost, false)
	v.SetDefault(types.DistroUpdateFirewall, false)
}

// GetStringSlice returns a string slice from viper
// it consistently returns expected results across flags and environment variables
// https://github.com/spf13/viper/issues/380
func GetStringSlice(v *viper.Viper, key string) []string {
	var result []string
	if err := v.UnmarshalKey(key, &result); err != nil {
		return nil
	}
	return result
}
