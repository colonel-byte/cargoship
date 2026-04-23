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
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostsEach(t *testing.T) {
	hosts := ZarfHosts{
		&ZarfHost{Role: "controller"},
		&ZarfHost{Role: "worker"},
	}

	t.Run("success", func(t *testing.T) {
		var roles []string
		fn := func(_ context.Context, h *ZarfHost) error {
			roles = append(roles, h.Role)
			return nil
		}
		err := hosts.Each(context.Background(), fn)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"controller", "worker"}, roles)
		require.Len(t, roles, 2)
	})

	t.Run("context cancel", func(t *testing.T) {
		var count int
		ctx, cancel := context.WithCancel(context.Background())

		fn := func(ctx context.Context, h *ZarfHost) error {
			count++
			cancel()
			return nil
		}
		err := hosts.Each(ctx, fn)
		require.Equal(t, 1, count)
		require.Error(t, err)
		require.ErrorContains(t, err, "cancel")
	})

	t.Run("error", func(t *testing.T) {
		fn := func(_ context.Context, h *ZarfHost) error {
			return errors.New("test")
		}
		err := hosts.Each(context.Background(), fn)
		require.Error(t, err)
		require.ErrorContains(t, err, "test")
	})
}
