// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package zarfflags

import (
	"strings"
	"testing"
)

// helpFixture is the shape cobra renders, trimmed to the lines that matter: a flag with a short
// form, one without, a repeated name across the two sections, and prose that mentions a flag in
// the middle of a sentence.
const helpFixture = `Injects an OCI registry into a Kubernetes cluster.

Usage:
  zarf init [ PACKAGE_SOURCE ] [flags]

Examples:
  # Initializing w/ a custom init package:
  $ zarf init /srv/staging/zarf-init-amd64-v0.86.0.tar.zst --confirm

Flags:
      --components string        Comma-separated list of components
  -c, --confirm                  Confirm the action
      --storage-class string     The storage class to use. Pass --storage-class="" to unset it.
  -h, --help                     help for init

Global Flags:
  -a, --architecture string      Architecture for OCI images
      --confirm                  Confirm the action
      --log-level string         Log level
      --no-color                 Disable colors
`

func TestParseHelp(t *testing.T) {
	got := parseHelp(helpFixture)

	want := []string{
		// keep-sorted start
		"architecture",
		"components",
		"confirm",
		"help",
		"log-level",
		"no-color",
		"storage-class",
		// keep-sorted end
	}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("parseHelp returned\n  %v\nwant\n  %v", got, want)
	}
}

// TestParseHelpIgnoresProse pins the one false positive that would matter: a flag named inside a
// description, which is where zarf documents the empty-string form of several of its own flags. A
// parser that took those would record flags zarf does not accept, and the tests reading this list
// would then pass over a module rendering one.
func TestParseHelpIgnoresProse(t *testing.T) {
	for _, flag := range parseHelp(helpFixture) {
		if flag == "storage-class=" || flag == "storage-class=\"\"" {
			t.Errorf("parseHelp took %q out of a description", flag)
		}
	}
}

// TestTargetsCoverEveryRecordedList is what notices a module added to internal/zarfmod with no
// entry here: the file it reads would never be regenerated, so the list it holds would quietly
// describe whatever zarf was installed the day it was written.
func TestTargetsCoverEveryRecordedList(t *testing.T) {
	targets := targets()
	if len(targets) == 0 {
		t.Fatal("no targets, so generate:zarfFlags would write nothing")
	}

	for _, target := range targets {
		if len(target.args) == 0 {
			t.Errorf("%s has no command to read", target.file)
		}
		if target.file == "" || target.test == "" || target.docs == "" {
			t.Errorf("%v is missing its file, test or docs reference", target.args)
		}
	}
}
