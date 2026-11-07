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
	"net"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/pkg/coci"
	"github.com/spf13/cobra"
)

// staticArchSuggestion is one of the descriptions the generic architecture list carries, which
// the artifact-derived suggestions never do. It is how these tests tell the two apart.
const staticArchSuggestion = "the x86-64, 64-bit AMD, architecture"

func isStaticList(suggestions []string) bool {
	for _, s := range suggestions {
		if strings.Contains(s, staticArchSuggestion) {
			return true
		}
	}
	return false
}

// A completion must always answer. Every one of these cases is a reason the reference cannot
// be read, and each has to fall back to the architectures cargoship builds for rather than
// offering nothing -- a completion is not the place to learn a registry is unreachable.
func TestCompleteArchFallsBackToTheStaticList(t *testing.T) {
	// A listener that is closed immediately, so a connection to it is refused rather than
	// hanging. This keeps the unreachable-registry case instant instead of waiting out
	// completionTimeout.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	closedAddr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	for _, tc := range []struct {
		name string
		args []string
	}{
		{
			name: "no reference typed yet",
			args: nil,
		},
		{
			name: "a local path",
			args: []string{"./artifact.tar.gz"},
		},
		{
			name: "an https URL",
			args: []string{"https://example.com/artifact.tar.gz"},
		},
		{
			name: "a registry that refuses the connection",
			args: []string{"oci://" + closedAddr + "/vendor/engine:v1"},
		},
		{
			name: "a reference that does not parse",
			args: []string{"oci://not a reference"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := &sha256SumOptions{}
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())

			suggestions, directive := o.completeArch(cmd, tc.args, "")
			if !isStaticList(suggestions) {
				t.Errorf("completeArch returned %v, want the static architecture list", suggestions)
			}
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Errorf("directive = %v, want ShellCompDirectiveNoFileComp; a file listing is never a valid architecture", directive)
			}
		})
	}
}

// The architectures read off an artifact are rendered with the platform the artifact declares,
// and narrowed to what has been typed.
func TestArchSuggestions(t *testing.T) {
	// What ghcr.io/cri-o/bundle and ghcr.io/zarf-dev/packages/init respectively hold.
	vendor := []coci.ArtifactPlatform{
		{Arch: "amd64", OS: "linux"},
		{Arch: "arm64", OS: "linux"},
		{Arch: "ppc64le", OS: "linux"},
	}
	zarf := []coci.ArtifactPlatform{
		{Arch: "amd64", OS: "multi"},
		{Arch: "skeleton", OS: "multi"},
	}

	for _, tc := range []struct {
		name       string
		platforms  []coci.ArtifactPlatform
		toComplete string
		want       []string
	}{
		{
			name:      "every architecture a vendor bundle offers",
			platforms: vendor,
			want:      []string{"amd64\tlinux/amd64", "arm64\tlinux/arm64", "ppc64le\tlinux/ppc64le"},
		},
		{
			name:       "narrowed to what has been typed",
			platforms:  vendor,
			toComplete: "a",
			want:       []string{"amd64\tlinux/amd64", "arm64\tlinux/arm64"},
		},
		{
			name:       "a prefix nothing matches",
			platforms:  vendor,
			toComplete: "riscv",
			want:       []string{},
		},
		{
			name:      "a zarf index keeps its multi spelling and its skeleton child",
			platforms: zarf,
			want:      []string{"amd64\tmulti/amd64", "skeleton\tmulti/skeleton"},
		},
		{
			name:      "nothing offered",
			platforms: nil,
			want:      []string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := archSuggestions(tc.platforms, tc.toComplete)
			if len(got) != len(tc.want) {
				t.Fatalf("archSuggestions = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("suggestion %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// A listing that had to assume an architecture says so on every entry, not only in the
// ActiveHelp line: cobra's generated fish script runs __complete with ACTIVE_HELP=0, so the
// description is the only form a fish user ever sees.
func TestLayerSuggestions(t *testing.T) {
	titles := []string{"components/zarf-injector.tar", "components/zarf-registry.tar", "images/index.json"}

	for _, tc := range []struct {
		name       string
		listing    coci.LayerListing
		toComplete string
		want       []string
	}{
		{
			name:    "an architecture was given, so nothing is assumed",
			listing: coci.LayerListing{Titles: titles, Arch: "amd64"},
			want:    titles,
		},
		{
			name:       "narrowed to what has been typed",
			listing:    coci.LayerListing{Titles: titles, Arch: "amd64"},
			toComplete: "components/",
			want:       []string{"components/zarf-injector.tar", "components/zarf-registry.tar"},
		},
		{
			name:    "an assumed architecture is carried on every entry",
			listing: coci.LayerListing{Titles: titles[:1], Arch: "amd64", Assumed: true},
			want:    []string{"components/zarf-injector.tar\tfrom the amd64 manifest; pass --arch"},
		},
		{
			name:    "no titles",
			listing: coci.LayerListing{},
			want:    []string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := layerSuggestions(tc.listing, tc.toComplete)
			if len(got) != len(tc.want) {
				t.Fatalf("layerSuggestions = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("suggestion %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// Suggesting members means reading the layer itself, not a manifest, so completion only does
// it once the layer to read is not in question. Each of these would otherwise have cargoship
// download tens of mebibytes on a tab press -- or guess a platform and download the wrong one.
func TestCompleteExtractPathOffersNothingUntilTheLayerIsChosen(t *testing.T) {
	const ref = "oci://ghcr.io/zarf-dev/packages/init:v0.89.0"

	for _, tc := range []struct {
		name string
		args []string
		o    sha256SumOptions
	}{
		{
			name: "no reference typed yet",
			o:    sha256SumOptions{arch: "amd64", layer: "components/zarf-injector.tar"},
		},
		{
			name: "a local path has no layer to read",
			args: []string{"./artifact.tar.gz"},
			o:    sha256SumOptions{arch: "amd64", layer: "components/zarf-injector.tar"},
		},
		{
			name: "no layer named",
			args: []string{ref},
			o:    sha256SumOptions{arch: "amd64"},
		},
		{
			name: "no architecture named, so the platform would have to be assumed",
			args: []string{ref},
			o:    sha256SumOptions{layer: "components/zarf-injector.tar"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())

			suggestions, directive := tc.o.completeExtractPath(cmd, tc.args, "")
			if len(suggestions) != 0 {
				t.Errorf("completeExtractPath returned %v, want nothing", suggestions)
			}
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Errorf("directive = %v, want ShellCompDirectiveNoFileComp", directive)
			}
		})
	}
}
