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

	"github.com/stretchr/testify/require"
)

func TestLoadEmptySource(t *testing.T) {
	_, err := Load(context.Background(), "", LoadOptions{})
	require.Error(t, err)
	require.ErrorContains(t, err, "must provide a package source")
}

func TestLoadUnsupportedSourceType(t *testing.T) {
	// "foo.part000" is identified by utils.IdentifySource as a "split" source, which
	// Load's switch has no case for, so it falls through to the unsupported-source error
	// without touching the network or filesystem.
	_, err := Load(context.Background(), "foo.part000", LoadOptions{})
	require.Error(t, err)
	require.ErrorContains(t, err, "unsupported source type split")
}

func TestLoadUnidentifiableSource(t *testing.T) {
	_, err := Load(context.Background(), "not a valid source at all!!", LoadOptions{})
	require.Error(t, err)
	require.ErrorContains(t, err, "unknown source")
}
