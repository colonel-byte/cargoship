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

// Package cmd is where the commands for cargoship
package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/cmd/flags"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/internal/logging"
	"github.com/colonel-byte/cargoship/pkg/utils"
	"github.com/colonel-byte/cargoship/types"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"golang.org/x/term"
)

const (
	// RootLoggingLevel command flag log level
	RootLoggingLevel = "log-level"
	// RootLoggingFormat command flag log format
	RootLoggingFormat = "log-format"
	// RootTimeout command flag timeout
	RootTimeout = "timeout"
	// RootZarfCache command flag zarf-cache
	RootZarfCache = "zarf-cache"
	// RootArchitecture command flag architecture
	RootArchitecture = "architecture"
	// RootPlainHTTP is the config-file key behind the --plain-http flag. It is a viper key, not
	// a flag name, which is why it is spelled in the snake_case the config file uses.
	RootPlainHTTP = "plain_http"
	// RootInsecureSkipTLSVerify is the config-file key behind the --insecure-skip-tls-verify
	// flag, spelled as the config file spells it for the same reason as RootPlainHTTP.
	RootInsecureSkipTLSVerify = "insecure_skip_tls_verify"
	// loggingLevelDefault is the log level used when neither a flag, env var,
	// config file, nor viper default resolves one.
	loggingLevelDefault = "info"
)

const (
	// InstallConfig flag
	InstallConfig = "config"
	// InstallTargetHosts flag
	InstallTargetHosts = "target-hosts"
	// InstallConfirm flag
	InstallConfirm = "confirm"
	// InstallDistro flag. Named directly by the commands that load no package -- reset and
	// kube-config -- which have nothing else to read the engine's identity from.
	InstallDistro = "distro"
	// InstallDryRun flag
	InstallDryRun = "dry-run"
	// InstallConcurrency flag
	InstallConcurrency = "concurrency"
	// InstallWorkConcurrency flag
	InstallWorkConcurrency = "work-concurrency"
	// InstallUpdateHost flag
	InstallUpdateHost = "hosts"
	// InstallUpdateFirewall flag
	InstallUpdateFirewall = "firewall"
	// InstallLabelNodes flag
	InstallLabelNodes = "label-nodes"
	// InstallAllowUnmanagedNodes flag
	InstallAllowUnmanagedNodes = "allow-unmanaged-nodes"
	// InstallAllowDowngrade flag
	InstallAllowDowngrade = "allow-downgrade"
	// InstallUpdateKubeConfig flag
	InstallUpdateKubeConfig = "update-kubeconfig"
	// InstallKubeConfigPath flag
	InstallKubeConfigPath = "kubeconfig"
	// InstallVaultPasswordFile flag
	InstallVaultPasswordFile = "vault-password-file"
	// InstallValues flag
	InstallValues = "values"
	// InstallReceiptPath flag
	InstallReceiptPath = "receipt-path"
)

const (
	// PackageOCIConcurrency flag
	PackageOCIConcurrency = "oci-concurrency"
	// PackageTag flag
	PackageTag = "tag"
)

const (
	// MiscOutput flag
	MiscOutput = "output"
	// MiscPackage flag
	MiscPackage = "package"
)

var (
	// IsColorDisabled whether to show the colored output
	IsColorDisabled bool
	// LogFormat format of the log output
	LogFormat string
	// LogLevelCLI log level
	LogLevelCLI string
	// LogFile enables always writing a full-verbosity debug log to a file
	LogFile bool
	// Timeout for how long a task runs
	Timeout string
	// logFile is the file handle backing the always-on debug log, if enabled. Held here so
	// Execute can close it once the command has finished running.
	logFile   *os.File
	distroCfg = types.DistroConfig{}
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

var rootCmd = NewCargoshipCommand()

// NewCargoshipCommand is the root command
func NewCargoshipCommand() *cobra.Command {
	err := initViper()
	if err != nil {
		// stderr, not stdout: this runs during package initialisation, so it can land in the
		// middle of a document a command was asked to write. See emitConfigError.
		emitConfigError(err)
	}

	rootCmd := &cobra.Command{
		Use:           lang.RootCmdUse,
		Short:         lang.RootCmdShort,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: preRun,
	}

	for _, g := range groups {
		rootCmd.AddGroup(g)
	}

	// Package related
	rootCmd.AddCommand(newPackageCreateCommand())
	rootCmd.AddCommand(newPackagePublishCommand())
	rootCmd.AddCommand(newPackageMirrorCommand())
	rootCmd.AddCommand(newPackagePullCommand())
	rootCmd.AddCommand(newPackageImportCommand())
	rootCmd.AddCommand(newPackageSignCommand())
	rootCmd.AddCommand(newPackageVerifyCommand())
	// Install related
	rootCmd.AddCommand(newInstallApplyCommand())
	rootCmd.AddCommand(newInstallPrepareCommand())
	rootCmd.AddCommand(newInstallResetCommand())
	rootCmd.AddCommand(newInstallKubeConfigCommand())
	rootCmd.AddCommand(newInstallEngineConfigSyncCommand())
	// Misc related
	rootCmd.AddCommand(newVersionCommand())
	rootCmd.AddCommand(newValidateCommand())
	// Groups
	rootCmd.AddCommand(newSchemaCommand())
	rootCmd.AddCommand(newInventoryCommand())
	rootCmd.AddCommand(newDevCommand())
	rootCmd.AddCommand(newVaultCommand())

	rootCmd.PersistentFlags().StringVarP(&LogLevelCLI, RootLoggingLevel, "l", resolvedConfig.LogLevel, lang.RootCmdFlagLogLevel)
	if err := rootCmd.RegisterFlagCompletionFunc(RootLoggingLevel, flags.RegisterLogLevel); err != nil {
		fmt.Fprintf(os.Stderr, "failed to register %s flag completion: %v\n", RootLoggingLevel, err)
	}
	rootCmd.PersistentFlags().StringVarP(&LogFormat, RootLoggingFormat, "L", resolvedConfig.LogFormat, lang.RootCmdFlagLogFormat)
	if err := rootCmd.RegisterFlagCompletionFunc(RootLoggingFormat, flags.RegisterLogFormat); err != nil {
		fmt.Fprintf(os.Stderr, "failed to register %s flag completion: %v\n", RootLoggingFormat, err)
	}
	rootCmd.PersistentFlags().BoolVar(&IsColorDisabled, "no-color", resolvedConfig.NoColor, lang.RootCmdFlagNoColor)
	rootCmd.PersistentFlags().BoolVar(&LogFile, "log-file", resolvedConfig.LogFile, lang.RootCmdFlagLogFile)

	return rootCmd
}

// Execute is the root execute logic
func Execute(ctx context.Context) error {
	defer func() {
		if logFile != nil {
			_ = logFile.Close() //nolint:errcheck
		}
	}()

	// A config file that was found and could not be used is fatal, but it is reported here rather
	// than where it was met. Package initialisation is too early to fail a process from: it runs
	// before main, so an os.Exit there ends an Ansible module run before the module has written
	// the one JSON object Ansible reads, and Ansible reports a module failure of its own devising
	// instead of the reason.
	// logger.Default is the fallback used below for an error met before preRun installed the real
	// logger -- an unparsed flag, a wrong argument count. Its ConfigDefault hardcodes Color: true,
	// so without this an error on that path wrote escape sequences into a redirected stream even
	// though every other line respected the terminal. --no-color cannot be consulted yet (flags
	// are unparsed), but NO_COLOR and the terminal check can, and they are what the redirect case
	// turns on.
	if !colorEnabled() {
		cfg := logger.ConfigDefault()
		cfg.Color = false
		if l, lerr := logger.New(cfg); lerr == nil {
			logger.SetDefault(l)
		}
	}

	if err := configLoadError; err != nil && !describesItself(os.Args[1:]) {
		// Already written to stderr by emitConfigError during init. Returning it without a second
		// report keeps one copy of what is now a multi-line message -- goccy annotates an unknown
		// key with the offending line and a caret, and printing that twice buries it. It also
		// avoids logger.Default(), which has no --no-color or terminal check applied: preRun never
		// ran on this path, so a redirected run would get escape sequences from it alone.
		return err
	}

	_, err := rootCmd.ExecuteContextC(ctx)
	if err == nil {
		return nil
	}
	// Use default logger in case there was an error prior to the logger being setup
	logger.Default().Error(err.Error())
	return err
}

// describesItself reports whether args ask cargoship only to describe itself rather than to do
// anything with a cluster or a package.
//
// Those invocations run even when the config file cannot be used. An unknown key is fatal, which
// is the point -- but it must not take the help with it: the message tells the operator to run
// "cargoship validate --kind config", and until this existed, the help that would have shown them
// that command exited 3 and printed nothing. Being locked out of the documentation by the thing
// the documentation explains is a poor trade for consistency.
//
// Nothing here reads the config, so none of them can act on a bad one. It matches on the raw
// argument vector because flags are not parsed yet at this point in Execute.
//
// A help flag counts wherever it appears. A subcommand name counts only in the first non-flag
// position, so "cargoship create --tag version" is not mistaken for the version command. A flag
// value sitting in that position ("--log-level debug version") is not recognised, which errors on
// the bad config as before -- the safe direction to be wrong in.
func describesItself(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}

	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		switch a {
		case "help", "completion", "version", "v":
			return true
		}
		return false
	}

	return false
}

// configLoadError is a config file that was found and could not be used. It is met during package
// initialisation and acted on in Execute, which is the first point in the process that can fail
// without taking the process with it.
var configLoadError error //nolint:gochecknoglobals // set once, in init, beside the viper globals

// emitConfigError reports a configuration file that could not be loaded.
//
// It writes to stderr, and it exists as a function so that both callers stay that way. Both run
// during package initialisation, before any flag is parsed and so before the real logger exists,
// which is early enough that the Ansible module mode in internal/ansiblemod cannot redirect
// os.Stdout ahead of them. Commands that emit a document on stdout -- schema, validate, inventory
// from-ansible -- would have that document corrupted by a warning mixed into it.
func emitConfigError(err error) {
	fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
}

// ExecuteArgs runs the root command against an explicit argument vector instead of os.Args.
//
// It exists for the Ansible module mode in internal/ansiblemod, which turns a module's JSON
// parameters into the flags an operator would have typed and then runs the ordinary command.
// Building an argument vector rather than calling into src/pkg/action directly is the point of the
// arrangement: keyring resolution, timeout parsing, package loading, and phase construction stay on
// one path, so what a playbook does and what an operator does cannot drift apart.
func ExecuteArgs(ctx context.Context, args []string) error {
	rootCmd.SetArgs(args)
	return Execute(ctx)
}

// PrintViperConfigUsed informs users when Zarf has detected a config file.
func PrintViperConfigUsed(ctx context.Context) error {
	l := logger.From(ctx)

	// Only print config info if viper is initialized.
	vInitialized := v != nil
	if !vInitialized {
		return nil
	}
	if cfgFile := v.ConfigFileUsed(); cfgFile != "" {
		l.Info("using config file", "location", cfgFile)
	}
	return nil
}

func init() {
	err := initViper()
	if err != nil {
		emitConfigError(err)
	}

	if v.ConfigFileUsed() != "" {
		if err := loadViperConfig(); err != nil {
			configLoadError = fmt.Errorf("unable to load the config file %s: %w", v.ConfigFileUsed(), err)
			emitConfigError(configLoadError)
		}
	}
}

func parsePath(ctx context.Context, value string) string {
	absValue, err := config.GetAbsHomePath(value)
	if err != nil {
		logger.From(ctx).Debug("error when trying to get user path", "error", err)
		return value
	}
	return absValue
}

func loadViperConfig() error {
	// get config file from Viper
	configFile, err := os.ReadFile(v.ConfigFileUsed())
	if err != nil {
		return err
	}

	err = unmarshalAndValidateConfig(configFile, &distroCfg)
	if err != nil {
		// The caller already names the file, so add only what the operator does next. validate
		// reports every bad key at once where a decode stops at the first, which is one run
		// instead of one per typo.
		return fmt.Errorf("%w\n\trun 'cargoship validate --kind config %s' to see every problem in the file",
			err, v.ConfigFileUsed())
	}

	return nil
}

// unmarshalAndValidateConfig decodes the config file strictly, to reject a key the struct does not
// define. The decoded value is deliberately discarded: resolvedConfig in viper.go is what supplies
// flag defaults, and distroCfg exists only so that the file is parsed against the real type. The
// decode is the validation.
func unmarshalAndValidateConfig(configFile []byte, distroCfg *types.DistroConfig) error {
	return utils.ReadByteStrict(configFile, distroCfg)
}

func preRun(cmd *cobra.Command, _ []string) error {
	logFilePath := ""
	if LogFile {
		logFilePath = defaultLogFilePath()
	}

	// Configure logger and add it to cmd context. setupLogger wants "isColor", which is the
	// inverse of the flag plus the two conditions colorEnabled covers.
	isColor := colorEnabled()
	l, f, err := setupLogger(LogLevelCLI, LogFormat, isColor, logFilePath)
	if err != nil {
		return err
	}
	logFile = f
	ctx := logger.WithContext(cmd.Context(), l)
	cmd.SetContext(ctx)

	// Keep PTerm's message prints on the same decision as the logger, so a redirected run has
	// no escape sequences from either.
	if !isColor {
		pterm.DisableColor()
	}

	// Print out config location
	err = PrintViperConfigUsed(cmd.Context())
	if err != nil {
		return err
	}

	if f != nil {
		l.Info("logging debug to", "path", f.Name())
	}

	l.Debug("using temporary directory", "tmpDir", config.CommonOptions.TempDirectory)
	return nil
}

// colorEnabled reports whether to emit terminal color codes. Three things turn color off, and
// any one of them is enough: --no-color (or its config key), the NO_COLOR environment variable
// per https://no-color.org, and stderr not being a terminal.
//
// Stderr is the stream that decides it because every log line goes there -- a run redirected to
// a file or piped into a pager previously left ANSI escapes in the output, since console-slog
// honours only the option it is handed and never looks at the file descriptor itself.
func colorEnabled() bool {
	if IsColorDisabled || os.Getenv("NO_COLOR") != "" {
		return false
	}
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// setupLogger handles creating a logger and setting it as the global default. When logFilePath
// is non-empty, every log level is additionally written to that file as JSON, regardless of
// what level is configured for the console -- the returned *os.File is the caller's to close
// once logging is done.
func setupLogger(level, format string, isColor bool, logFilePath string) (*slog.Logger, *os.File, error) {
	// If we didn't get a level from config, fallback to "info"
	if level == "" {
		level = "info"
	}
	sLevel, err := logger.ParseLevel(level)
	if err != nil {
		return nil, nil, err
	}
	cfg := logger.Config{
		Level:       sLevel,
		Format:      logger.Format(format),
		Destination: logger.DestinationDefault,
		Color:       logger.Color(isColor),
	}
	l, err := logger.New(cfg)
	if err != nil {
		return nil, nil, err
	}

	var f *os.File
	if logFilePath != "" {
		fileHandler, opened, err := logging.OpenFileHandler(logFilePath)
		if err != nil {
			// A log file we can't open shouldn't block the CLI from running; fall back to
			// console-only logging.
			l.Warn("failed to open log file, continuing without one", "path", logFilePath, "error", err)
		} else {
			f = opened
			l = slog.New(logging.NewMultiHandler(l.Handler(), fileHandler))
		}
	}

	logger.SetDefault(l)
	l.Debug("logger successfully initialized", "cfg", cfg, "logFile", logFilePath)
	return l, f, nil
}

// defaultLogFilePath returns where the always-on debug log file is written: under the same
// cache directory cargoship already uses for OCI artifacts, named to the hundredth of a
// second. Invocations started within the same hundredth of a second share/append to the same
// file since the name carries no PID or other disambiguator.
func defaultLogFilePath() string {
	cacheDir, err := config.GetAbsCachePath()
	if err != nil || cacheDir == "" {
		cacheDir = config.DefaultCachePath
	}
	name := fmt.Sprintf("cargoship-%s.log", time.Now().Format(config.TimeFormat))
	return filepath.Join(cacheDir, "logs", name)
}
