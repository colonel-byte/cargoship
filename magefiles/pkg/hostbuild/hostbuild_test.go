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

package hostbuild

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/colonel-byte/cargoship/config"
	"github.com/stretchr/testify/require"
)

// chdir points the process at dir for the rest of the test. GitCommit and Clean both read the
// working directory, so a test for either has to set one.
func chdir(t *testing.T, dir string) {
	t.Helper()

	previous, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previous)) })
}

// TestGitCommitOutsideACheckout covers the fallback: a source tree with no .git directory still
// stamps something rather than an empty string, which is what a released tarball builds from.
func TestGitCommitOutsideACheckout(t *testing.T) {
	chdir(t, t.TempDir())

	require.Equal(t, config.UnsetCLICommit, GitCommit())
}

// TestGitCommitInACheckout covers the other branch. The value depends on the checkout, so the
// assertion is on its shape: a short hash, optionally marked dirty, and never the fallback.
func TestGitCommitInACheckout(t *testing.T) {
	commit := GitCommit()

	require.NotEmpty(t, commit)
	require.Regexp(t, `^[0-9a-f]{7,}(-dirty)?$`, commit)
}

// TestCleanRemovesOnlyTheBuiltBinaries checks the glob: Clean is wired to Dev.Clean, and a
// pattern that reached wider would delete whatever else a developer keeps in build/.
func TestCleanRemovesOnlyTheBuiltBinaries(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	require.NoError(t, os.Mkdir(Dir, 0o755))
	built := []string{"cargoship_linux_amd64", "cargoship_darwin_arm64"}
	kept := []string{"tmp-notes.txt", "examples"}
	for _, name := range append(append([]string{}, built...), kept...) {
		require.NoError(t, os.WriteFile(filepath.Join(Dir, name), []byte("x"), 0o644))
	}

	require.NoError(t, Clean())

	for _, name := range built {
		require.NoFileExists(t, filepath.Join(Dir, name))
	}
	for _, name := range kept {
		require.FileExists(t, filepath.Join(Dir, name))
	}
}

// TestCleanWithNoBuildDir covers the case Dev.Clean hits on a fresh checkout: the glob matches
// nothing and Clean has to succeed rather than report the missing directory.
func TestCleanWithNoBuildDir(t *testing.T) {
	chdir(t, t.TempDir())

	require.NoError(t, Clean())
}

// TestPlatformFillsInTheHost covers the empty-value contract the build flags depend on: a flag
// left off the command line means this machine's own value for that axis.
func TestPlatformFillsInTheHost(t *testing.T) {
	for _, tt := range []struct {
		name       string
		oper, arch string
		wantOper   string
		wantArch   string
	}{
		{name: "both empty", wantOper: runtime.GOOS, wantArch: runtime.GOARCH},
		{name: "os only", oper: "windows", wantOper: "windows", wantArch: runtime.GOARCH},
		{name: "arch only", arch: "riscv64", wantOper: runtime.GOOS, wantArch: "riscv64"},
		{name: "neither empty", oper: "darwin", arch: "arm64", wantOper: "darwin", wantArch: "arm64"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			oper, arch := Platform(tt.oper, tt.arch)
			require.Equal(t, tt.wantOper, oper)
			require.Equal(t, tt.wantArch, arch)
		})
	}
}

// TestArgsAfter covers finding a target's own arguments on a command line whose leading flags
// belong to mage and may themselves take a value -- the case that makes counting flags useless.
func TestArgsAfter(t *testing.T) {
	for _, tt := range []struct {
		name string
		argv []string
		want []string
	}{
		{
			name: "no arguments of its own",
			argv: []string{"mage", "build:binary"},
			want: []string{},
		},
		{
			name: "flags of its own",
			argv: []string{"mage", "build:binary", "-os=windows"},
			want: []string{"-os=windows"},
		},
		{
			// -d takes a value, so a target that counted leading flags would mistake
			// "magefiles" for the target name and return the wrong slice.
			name: "after a mage flag that takes a value",
			argv: []string{"mage", "-d", "magefiles", "build:binary", "-arch=arm64"},
			want: []string{"-arch=arm64"},
		},
		{
			name: "target lookup is case-insensitive, like mage's own",
			argv: []string{"mage", "Build:Binary", "-os=darwin"},
			want: []string{"-os=darwin"},
		},
		{
			name: "a different target entirely",
			argv: []string{"mage", "dev:clean"},
			want: nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ArgsAfter(tt.argv, "build:binary"))
		})
	}
}

func TestParsePlatform(t *testing.T) {
	for _, tt := range []struct {
		name         string
		args         []string
		wantOper     string
		wantArch     string
		wantConsumed int
	}{
		{
			name:     "no flags is the host",
			args:     nil,
			wantOper: runtime.GOOS, wantArch: runtime.GOARCH,
		},
		{
			name:     "os only keeps the host's arch",
			args:     []string{"-os=windows"},
			wantOper: "windows", wantArch: runtime.GOARCH, wantConsumed: 1,
		},
		{
			name:     "both flags",
			args:     []string{"-os=darwin", "-arch=arm64"},
			wantOper: "darwin", wantArch: "arm64", wantConsumed: 2,
		},
		{
			name:     "space-separated and double-dashed spellings",
			args:     []string{"--os", "linux", "--arch", "arm64"},
			wantOper: "linux", wantArch: "arm64", wantConsumed: 4,
		},
		{
			// Parsing stops at the first non-flag, so a target chained after this one is
			// left alone and not consumed.
			name:     "stops at a following target",
			args:     []string{"-os=windows", "dev:clean"},
			wantOper: "windows", wantArch: runtime.GOARCH, wantConsumed: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			oper, arch, consumed, err := ParsePlatform(tt.args)
			require.NoError(t, err)
			require.Equal(t, tt.wantOper, oper)
			require.Equal(t, tt.wantArch, arch)
			require.Equal(t, tt.wantConsumed, consumed)
		})
	}
}

// TestParsePlatformRejectsAnUnknownFlag checks the error names the accepted flags, since a
// mistyped one is the likeliest way to reach it and mage prints nothing else about the target.
func TestParsePlatformRejectsAnUnknownFlag(t *testing.T) {
	_, _, consumed, err := ParsePlatform([]string{"-goos=windows"})
	require.Error(t, err)
	require.Zero(t, consumed)
	require.Contains(t, err.Error(), "-goos")
	require.Contains(t, err.Error(), "-os=<goos>")
}
