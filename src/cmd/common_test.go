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
	"testing"

	"github.com/colonel-byte/cargoship/src/config"
)

func TestGetCachePathInvalidCharsFallsBack(t *testing.T) {
	ctx := context.Background()
	orig := config.CommonOptions.CachePath
	config.CommonOptions.CachePath = "invalid@@path"
	t.Cleanup(func() { config.CommonOptions.CachePath = orig })

	got, err := getCachePath(ctx)
	if err != nil {
		t.Fatalf("getCachePath failed: %v", err)
	}

	// Expect CommonOptions.CachePath reset to DefaultCachePath and returned path absolute.
	if config.CommonOptions.CachePath != config.DefaultCachePath {
		t.Fatalf("CommonOptions.CachePath not reset: got %q, want %q", config.CommonOptions.CachePath, config.DefaultCachePath)
	}

	absDefault, err := config.GetAbsHomePath(config.DefaultCachePath)
	if err != nil {
		t.Fatalf("GetAbsHomePath failed: %v", err)
	}
	if got != absDefault {
		t.Fatalf("getCachePath: got %q, want %q", got, absDefault)
	}
}

func TestDefaultRemoteOptionsReflectGlobals(t *testing.T) {
	origPlainHTTP, origInsecure := plainHTTP, insecureSkipTLSVerify
	t.Cleanup(func() {
		plainHTTP, insecureSkipTLSVerify = origPlainHTTP, origInsecure
	})

	plainHTTP = true
	insecureSkipTLSVerify = true

	opts := defaultRemoteOptions()
	if !opts.PlainHTTP {
		t.Fatalf("defaultRemoteOptions PlainHTTP false, want true")
	}
	if !opts.InsecureSkipTLSVerify {
		t.Fatalf("defaultRemoteOptions InsecureSkipTLSVerify false, want true")
	}

	plainHTTP = false
	insecureSkipTLSVerify = false

	opts2 := defaultRemoteOptions()
	if opts2.PlainHTTP {
		t.Fatalf("defaultRemoteOptions PlainHTTP true after reset, want false")
	}
	if opts2.InsecureSkipTLSVerify {
		t.Fatalf("defaultRemoteOptions InsecureSkipTLSVerify true after reset, want false")
	}
}
