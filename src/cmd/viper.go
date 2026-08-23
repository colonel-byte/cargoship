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
	"reflect"
	"strings"

	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/types"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

var (
	v            *viper.Viper
	vConfigError error

	// resolvedConfig holds config values resolved through viper's normal
	// defaults > env > config-file precedence pipeline (NOT flags -- flags are
	// never bound back into viper). It is distinct from distroCfg in root.go,
	// which is populated by a separate, stricter, non-viper YAML parse that
	// doesn't see defaults or env vars. Populated once, inside initViper(),
	// which itself only runs once per process (guarded by `if v != nil`).
	resolvedConfig types.DistroConfig
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
	}

	v.SetEnvPrefix("distro")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Must run before ReadInConfig/Unmarshal below: v.Unmarshal only discovers a key
	// that's already "known" to viper (via SetDefault, a config-file entry, or an
	// explicit Set) -- see the comment on setDefaults for why this matters for
	// env-var-only values. Calling this after Unmarshal would silently break env-var
	// support for every key not also present in the user's config file.
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
		// A missing config file is expected when running on flags/env/defaults alone; ignore it.
		// Any other error (e.g. malformed YAML, permission denied) means a config file was found
		// but couldn't be used, so surface it instead of silently falling back to defaults.
		if notFoundErr, ok := errors.AsType[viper.ConfigFileNotFoundError](vConfigError); !ok {
			log.Warn(lang.CmdViperErrLoadingConfigFile, "error", vConfigError)
		} else {
			log.Debug(lang.CmdViperErrLoadingConfigFile, "error", notFoundErr)
		}
	}

	// Populated regardless of vConfigError above: defaults/env vars still need to
	// resolve even when no config file was found. Never fatal -- a bad unmarshal
	// just means flags fall back to their Go zero values as the viper-seed default.
	if err := v.Unmarshal(&resolvedConfig); err != nil {
		log.Warn(lang.CmdViperErrLoadingConfigFile, "error", err)
	}

	return nil
}

// configPath derives a viper dot-path key for a field in types.DistroConfig by
// reading the same struct tags v.Unmarshal(&resolvedConfig) decodes against, so the
// key can never silently drift from what actually gets populated. fieldNames is the
// chain of Go struct field names from DistroConfig down to the target field, e.g.
// configPath("DistroOpts", "OCIConcurrency") -> "distro.oci_concurrency".
//
// A field excluded from Unmarshal (mapstructure:"-", see config.go) falls back to its
// json tag, which still names the same viper key -- those fields are read directly via
// raw v.Get/v.IsSet calls elsewhere, but the key itself still comes from one place.
//
// Panics on a bad field chain: this only ever runs against the fixed types.DistroConfig
// shape during startup, so a mismatch is a programming error that should fail loud
// immediately, not silently resolve to an empty/wrong key.
func configPath(fieldNames ...string) string {
	t := reflect.TypeFor[types.DistroConfig]()
	parts := make([]string, 0, len(fieldNames))
	for _, name := range fieldNames {
		field, ok := t.FieldByName(name)
		if !ok {
			panic(fmt.Sprintf("configPath: no field %q in %s", name, t))
		}
		key := field.Tag.Get("mapstructure")
		if key == "" || key == "-" {
			key, _, _ = strings.Cut(field.Tag.Get("json"), ",")
		}
		if key == "" || key == "-" {
			panic(fmt.Sprintf("configPath: field %q has no usable mapstructure or json tag", name))
		}
		parts = append(parts, key)
		t = field.Type
	}
	return strings.Join(parts, ".")
}

func setDefaults() {
	v.SetDefault(configPath("LogLevel"), loggingLevelDefault)
	v.SetDefault(configPath("CachePath"), config.ZarfDefaultCachePath)
	v.SetDefault(configPath("LogFormat"), string(logger.FormatConsole))
	v.SetDefault(configPath("TempDirectory"), "/tmp")
	v.SetDefault(configPath("NoColor"), false)

	v.SetDefault(configPath("DistroOpts", "OCIConcurrency"), 6)
	v.SetDefault(configPath("DistroOpts", "CreateOpts", "SkipSBOM"), false)
	v.SetDefault(configPath("DistroOpts", "Concurrency"), 30)
	v.SetDefault(configPath("DistroOpts", "HostUpdate"), false)
	v.SetDefault(configPath("DistroOpts", "FirewallUpdate"), false)

	// The keys below have no real default value beyond the Go zero value -- they're
	// registered anyway (not skipped) because v.Unmarshal(&resolvedConfig) only picks
	// up a key that's "known" to viper (via SetDefault, a config-file entry, or an
	// explicit Set). A key set *only* through an environment variable is invisible to
	// Unmarshal without this: AutomaticEnv resolves it fine for a direct v.GetString
	// call, but Unmarshal builds its result from v.AllKeys(), which never learns about
	// an env-only key on its own. Confirmed via a standalone reproduction before adding
	// these -- omitting any of them silently drops that key's env-var support.
	v.SetDefault(configPath("Architecture"), "")
	v.SetDefault(configPath("DistroOpts", "CreateOpts", "RegistryOverride"), []string{})
	v.SetDefault(configPath("DistroOpts", "FAPolicyd"), false)
	v.SetDefault(configPath("DistroOpts", "WorkerConcurrency"), 0)
	v.SetDefault(configPath("DistroOpts", "Type"), "")
	v.SetDefault(configPath("DistroOpts", "Retry"), 0)
	v.SetDefault(configPath("DistroOpts", "PublishOpts", "SigningKey"), "")
	v.SetDefault(configPath("DistroOpts", "PublishOpts", "SigningKeyPassword"), "")
	v.SetDefault(configPath("DistroOpts", "CertificateIdentity"), "")
	v.SetDefault(configPath("DistroOpts", "CertificateIdentityRegexp"), "")
	v.SetDefault(configPath("DistroOpts", "CertificateOIDCIssuer"), "")
	v.SetDefault(configPath("DistroOpts", "CertificateOIDCIssuerRegexp"), "")
	v.SetDefault(configPath("DistroOpts", "TrustedRoot"), "")
	v.SetDefault(configPath("DistroOpts", "PublicKey"), "")
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

type outputFormat string

const (
	outputJSON outputFormat = "json"
	outputYAML outputFormat = "yaml"
)

// must implement this interface for cmd.Flags().VarP
var _ pflag.Value = (*outputFormat)(nil)

func (o *outputFormat) Set(s string) error {
	switch s {
	case string(outputJSON), string(outputYAML):
		*o = outputFormat(s)
		return nil
	default:
		return fmt.Errorf("invalid output format: %s", s)
	}
}

func (o *outputFormat) String() string {
	return string(*o)
}

func (o *outputFormat) Type() string {
	return "outputFormat"
}
