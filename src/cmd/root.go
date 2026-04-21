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
	"fmt"
	"log/slog"
	"os"

	"github.com/colonel-byte/mare/src/config"
	"github.com/colonel-byte/mare/src/config/lang"
	"github.com/colonel-byte/mare/src/pkg/utils"
	"github.com/colonel-byte/mare/src/types"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	zarf "github.com/zarf-dev/zarf/src/cmd"
	zconfig "github.com/zarf-dev/zarf/src/config"
	zlang "github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	ROOT_LOGGING_LEVEL   = "log-level"
	ROOT_LOGGING_FORMART = "log-format"
	ROOT_TIMEOUT         = "timeout"
)

var (
	//keep-sorted start
	IsColorDisabled bool
	LogFormat       string
	LogLevelCLI     string
	Timeout         string
	distroCfg       = types.DistroConfig{}
	//keep-sorted end
)

var (
	groups = []*cobra.Group{
		{
			ID:    lang.RootGroupPackageID,
			Title: lang.RootGroupPackageTitle,
		},
		{
			ID:    lang.RootGroupInstallID,
			Title: lang.RootGroupInstallTitle,
		},
	}
)

var rootCmd = NewZarfDistroCommand()

func NewZarfDistroCommand() *cobra.Command {
	err := initViper()
	if err != nil {
		fmt.Printf("failed to load config: %v", err)
	}

	rootCmd := &cobra.Command{
		Use:           lang.RootCmdUse,
		Short:         lang.RootCmdShort,
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, _ []string) {
			err := cmd.Help()
			if err != nil {
				_, _ = fmt.Fprintln(os.Stderr, err)
			}
		},
		PersistentPreRunE: preRun,
	}

	for _, g := range groups {
		rootCmd.AddGroup(g)
	}

	rootCmd.AddCommand(newPackageCreateCommand())
	rootCmd.AddCommand(newInstallApplyCommand())

	rootCmd.PersistentFlags().StringVarP(&LogLevelCLI, ROOT_LOGGING_LEVEL, "l", v.GetString(zarf.VLogLevel), lang.RootCmdFlagLogLevel)
	rootCmd.PersistentFlags().StringVarP(&LogFormat, ROOT_LOGGING_FORMART, "L", v.GetString(zarf.VLogFormat), lang.RootCmdFlagLogFormat)
	rootCmd.PersistentFlags().StringVar(&Timeout, ROOT_TIMEOUT, v.GetString(ROOT_TIMEOUT), lang.CmdInstallFlagTimeout)
	rootCmd.PersistentFlags().BoolVar(&IsColorDisabled, "no-color", v.GetBool(zarf.VNoColor), lang.RootCmdFlagNoColor)
	rootCmd.PersistentFlags().StringVar(&config.CommonOptions.CachePath, "zarf-cache", parsePath(rootCmd.Context(), zarf.VZarfCache), zlang.RootCmdFlagCachePath)
	rootCmd.PersistentFlags().StringVar(&config.CommonOptions.TempDirectory, "tmpdir", parsePath(rootCmd.Context(), zarf.VTmpDir), zlang.RootCmdFlagTempDir)
	rootCmd.PersistentFlags().StringVarP(&zconfig.CLIArch, "architecture", "a", v.GetString(zarf.VArchitecture), zlang.RootCmdFlagArch)

	return rootCmd
}

func Execute(ctx context.Context) error {
	_, err := rootCmd.ExecuteContextC(ctx)
	if err == nil {
		return nil
	}
	// Use default logger in case there was an error prior to the logger being setup
	logger.Default().Error(err.Error())
	return err
}

// PrintViperConfigUsed informs users when Zarf has detected a config file.
func PrintViperConfigUsed(ctx context.Context) error {
	l := logger.From(ctx)

	// Only print config info if viper is initialized.
	vInitialized := v != nil
	if !vInitialized {
		return nil
	}
	if vConfigError != nil {
		return fmt.Errorf("unable to load config file: %w", vConfigError)
	}
	if cfgFile := v.ConfigFileUsed(); cfgFile != "" {
		l.Info("using config file", "location", cfgFile)
	}
	return nil
}

func init() {
	err := initViper()
	if err != nil {
		fmt.Printf("failed to load config: %v", err)
	}

	if v.ConfigFileUsed() != "" {
		if err := loadViperConfig(); err != nil {
			os.Exit(1)
		}
	}
}

func parsePath(ctx context.Context, key string) string {
	value, err := zconfig.GetAbsHomePath(v.GetString(key))
	if err != nil {
		logger.From(ctx).Debug("error when trying to get user path", "error", err)
		return v.GetString(key)
	}
	return value
}

func loadViperConfig() error {
	// get config file from Viper
	configFile, err := os.ReadFile(v.ConfigFileUsed())
	if err != nil {
		return err
	}

	err = unmarshalAndValidateConfig(configFile, &distroCfg)
	if err != nil {
		return err
	}

	return nil
}

func unmarshalAndValidateConfig(configFile []byte, distroCfg *types.DistroConfig) error {
	err := utils.ReadByteStrict(configFile, &distroCfg)
	if err != nil {
		return err
	}
	return nil
}

func preRun(cmd *cobra.Command, _ []string) error {
	// Configure logger and add it to cmd context. We flip NoColor because setLogger wants "isColor"
	l, err := setupLogger(LogLevelCLI, LogFormat, !IsColorDisabled)
	if err != nil {
		return err
	}
	ctx := logger.WithContext(cmd.Context(), l)
	cmd.SetContext(ctx)

	// if --no-color is set, disable PTerm color in message prints
	if IsColorDisabled {
		pterm.DisableColor()
	}

	// Print out config location
	err = PrintViperConfigUsed(cmd.Context())
	if err != nil {
		return err
	}

	l.Debug("using temporary directory", "tmpDir", config.CommonOptions.TempDirectory)
	return nil
}

// setupLogger handles creating a logger and setting it as the global default.
func setupLogger(level, format string, isColor bool) (*slog.Logger, error) {
	// If we didn't get a level from config, fallback to "info"
	if level == "" {
		level = "info"
	}
	sLevel, err := logger.ParseLevel(level)
	if err != nil {
		return nil, err
	}
	cfg := logger.Config{
		Level:       sLevel,
		Format:      logger.Format(format),
		Destination: logger.DestinationDefault,
		Color:       logger.Color(isColor),
	}
	l, err := logger.New(cfg)
	if err != nil {
		return nil, err
	}
	logger.SetDefault(l)
	l.Debug("logger successfully initialized", "cfg", cfg)
	return l, nil
}
