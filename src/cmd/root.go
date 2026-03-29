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

	"github.com/colonel-byte/zarf-distro/src/config/lang"
	"github.com/colonel-byte/zarf-distro/src/types"
	goyaml "github.com/goccy/go-yaml"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

var (
	logLevel  string
	distroCfg = types.DistroConfig{}
)

var rootCmd = &cobra.Command{
	Use:           lang.RootCmdUse,
	Short:         lang.RootCmdShort,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		_, _ = fmt.Fprintln(os.Stderr)
		err := cmd.Help()
		if err != nil {
			return errors.New("error calling help command")
		}
		return nil
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err == nil {
		return
	}
	pterm.Error.Println(err.Error())
	os.Exit(1)
}

// RootCmd returns the root command.
func RootCmd() *cobra.Command {
	return rootCmd
}

func init() {
	log, err := logger.New(logger.ConfigDefault())
	if err != nil {
		fmt.Printf("failed to create logger: %v", err)
	}

	err = initViper()
	if err != nil {
		fmt.Printf("failed to load config: %v", err)
	}

	if v.ConfigFileUsed() != "" {
		if err := loadViperConfig(); err != nil {
			log.Warn("failed to load zarf-distro-config", "error", err.Error())
			os.Exit(1)
		}
	}

	rootCmd.AddCommand(createCmd)
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
	// read relevant config into DeployOpts.Variables
	// need to use goyaml because Viper doesn't preserve case: https://github.com/spf13/viper/issues/1014
	// unmarshalling into DeployOpts because we want to check all of the top level config keys which are currently defined in DeployOpts
	err := goyaml.UnmarshalWithOptions(configFile, &distroCfg.DeployOpts, goyaml.Strict())
	if err != nil {
		return err
	}
	// validate config options
	for optionName := range distroCfg.DeployOpts.Options {
		if !isValidConfigOption(optionName) {
			return fmt.Errorf("invalid config option: %s", optionName)
		}
	}
	return nil
}
