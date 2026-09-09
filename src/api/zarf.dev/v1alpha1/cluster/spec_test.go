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

package cluster

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZarfClusterProfilesResolveConcurrency(t *testing.T) {
	t.Run("empty falls back", func(t *testing.T) {
		p := ZarfClusterProfiles{}
		v, err := p.ResolveConcurrency(10, "5")
		require.NoError(t, err)
		require.Equal(t, 5, v)
	})

	t.Run("empty falls back to percentage", func(t *testing.T) {
		p := ZarfClusterProfiles{}
		v, err := p.ResolveConcurrency(10, "25%")
		require.NoError(t, err)
		require.Equal(t, 3, v)
	})

	t.Run("fixed count is used as-is", func(t *testing.T) {
		p := ZarfClusterProfiles{Concurrency: "1"}
		v, err := p.ResolveConcurrency(10, "5")
		require.NoError(t, err)
		require.Equal(t, 1, v)
	})

	t.Run("fixed zero means unlimited", func(t *testing.T) {
		p := ZarfClusterProfiles{Concurrency: "0"}
		v, err := p.ResolveConcurrency(10, "5")
		require.NoError(t, err)
		require.Equal(t, 0, v)
	})

	t.Run("percentage scales and rounds up", func(t *testing.T) {
		p := ZarfClusterProfiles{Concurrency: "25%"}
		v, err := p.ResolveConcurrency(10, "5")
		require.NoError(t, err)
		require.Equal(t, 3, v)
	})

	t.Run("percentage clamps to a minimum of 1", func(t *testing.T) {
		p := ZarfClusterProfiles{Concurrency: "25%"}
		v, err := p.ResolveConcurrency(3, "5")
		require.NoError(t, err)
		require.Equal(t, 1, v)
	})

	t.Run("100 percent covers all hosts", func(t *testing.T) {
		p := ZarfClusterProfiles{Concurrency: "100%"}
		v, err := p.ResolveConcurrency(7, "5")
		require.NoError(t, err)
		require.Equal(t, 7, v)
	})

	t.Run("negative fixed count is an error", func(t *testing.T) {
		p := ZarfClusterProfiles{Concurrency: "-1"}
		_, err := p.ResolveConcurrency(10, "5")
		require.Error(t, err)
	})

	t.Run("out of range percentage is an error", func(t *testing.T) {
		p := ZarfClusterProfiles{Concurrency: "150%"}
		_, err := p.ResolveConcurrency(10, "5")
		require.Error(t, err)
	})

	t.Run("non-numeric value is an error", func(t *testing.T) {
		p := ZarfClusterProfiles{Concurrency: "abc"}
		_, err := p.ResolveConcurrency(10, "5")
		require.Error(t, err)
	})
}

func TestParseConcurrency(t *testing.T) {
	t.Run("empty means unlimited", func(t *testing.T) {
		v, err := ParseConcurrency("", 10)
		require.NoError(t, err)
		require.Equal(t, 0, v)
	})

	t.Run("fixed count is used as-is", func(t *testing.T) {
		v, err := ParseConcurrency("5", 10)
		require.NoError(t, err)
		require.Equal(t, 5, v)
	})

	t.Run("percentage scales and rounds up", func(t *testing.T) {
		v, err := ParseConcurrency("25%", 10)
		require.NoError(t, err)
		require.Equal(t, 3, v)
	})

	t.Run("negative fixed count is an error", func(t *testing.T) {
		_, err := ParseConcurrency("-1", 10)
		require.Error(t, err)
	})

	t.Run("non-numeric value is an error", func(t *testing.T) {
		_, err := ParseConcurrency("abc", 10)
		require.Error(t, err)
	})
}
