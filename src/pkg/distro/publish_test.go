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

package distro

import (
	"context"
	"testing"

	"oras.land/oras-go/v2/registry"

	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
	"github.com/zarf-dev/zarf/src/pkg/signing"
)

func TestPublishNilRegistry(t *testing.T) {
	ctx := context.Background()
	opts := PublishOptions{}
	var dst registry.Reference

	_, err := Publish(ctx, &layout.DistroLayout{}, dst, opts)
	if err == nil {
		t.Fatalf("Publish with nil Registry did not error")
	}
}

func TestPublishNegativeRetries(t *testing.T) {
	ctx := context.Background()
	dst := registry.Reference{Registry: "example.com", Repository: "repo"}
	opts := PublishOptions{
		Retries:         -1,
		Registry:        &dst,
		SignBlobOptions: signing.SignBlobOptions{},
	}

	_, err := Publish(ctx, &layout.DistroLayout{}, dst, opts)
	if err == nil {
		t.Fatalf("Publish with negative Retries did not error")
	}
}

func TestPublishNilLayout(t *testing.T) {
	ctx := context.Background()
	dst := registry.Reference{Registry: "example.com", Repository: "repo"}
	opts := PublishOptions{
		Retries:         1,
		Registry:        &dst,
		SignBlobOptions: signing.SignBlobOptions{},
	}

	_, err := Publish(ctx, nil, dst, opts)
	if err == nil {
		t.Fatalf("Publish with nil layout did not error")
	}
}

func TestPushToRemoteUsesPlatformAndOptions(t *testing.T) {
	ctx := context.Background()
	layout := &layout.DistroLayout{}
	layout.Distro.Metadata.Architecture = "amd64"
	ref := registry.Reference{Registry: "example.com", Repository: "repo"}
	opts := PublishOptions{
		OCIConcurrency: 1,
		Retries:        1,
		CachePath:      "\x00invalid",
	}

	// Expect pushToRemote to fail before contacting a real registry when cache path is invalid,
	// but call should still construct a platform without panic.
	err := pushToRemote(ctx, layout, ref, opts)
	if err == nil {
		t.Fatalf("pushToRemote with invalid cache path did not error")
	}
}
