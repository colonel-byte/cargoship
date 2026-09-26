// Copyright 2023 k0sctl authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
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

		fn := func(_ context.Context, _ *ZarfHost) error {
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
		fn := func(_ context.Context, _ *ZarfHost) error {
			return errors.New("test")
		}
		err := hosts.Each(context.Background(), fn)
		require.Error(t, err)
		require.ErrorContains(t, err, "test")
	})
}

func TestGroupByProfile(t *testing.T) {
	t.Run("groups by profile in first-appearance order", func(t *testing.T) {
		infra1 := &ZarfHost{Role: "worker", Profile: "infra"}
		worker1 := &ZarfHost{Role: "worker", Profile: "worker"}
		infra2 := &ZarfHost{Role: "worker", Profile: "infra"}
		none := &ZarfHost{Role: "worker"}
		worker2 := &ZarfHost{Role: "worker", Profile: "worker"}

		hosts := ZarfHosts{infra1, worker1, infra2, none, worker2}
		groups := hosts.GroupByProfile()

		require.Len(t, groups, 3)

		require.Equal(t, "infra", groups[0].Profile)
		require.Equal(t, ZarfHosts{infra1, infra2}, groups[0].Hosts)

		require.Equal(t, "worker", groups[1].Profile)
		require.Equal(t, ZarfHosts{worker1, worker2}, groups[1].Hosts)

		require.Empty(t, groups[2].Profile)
		require.Equal(t, ZarfHosts{none}, groups[2].Hosts)
	})

	t.Run("empty hosts returns no groups", func(t *testing.T) {
		var hosts ZarfHosts
		require.Empty(t, hosts.GroupByProfile())
	})
}
