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
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostsFirst(t *testing.T) {
	t.Run("returns first host", func(t *testing.T) {
		first := &ZarfHost{Role: "controller"}
		hosts := ZarfHosts{first, &ZarfHost{Role: "worker"}}
		require.Same(t, first, hosts.First())
	})

	t.Run("empty hosts returns nil", func(t *testing.T) {
		var hosts ZarfHosts
		require.Nil(t, hosts.First())
	})
}

func TestHostsLast(t *testing.T) {
	t.Run("returns last host", func(t *testing.T) {
		last := &ZarfHost{Role: "worker"}
		hosts := ZarfHosts{&ZarfHost{Role: "controller"}, last}
		require.Same(t, last, hosts.Last())
	})

	t.Run("empty hosts returns nil", func(t *testing.T) {
		var hosts ZarfHosts
		require.Nil(t, hosts.Last())
	})
}

func TestHostsFind(t *testing.T) {
	worker := &ZarfHost{Role: "worker"}
	hosts := ZarfHosts{&ZarfHost{Role: "controller"}, worker}

	t.Run("returns first match", func(t *testing.T) {
		found := hosts.Find(func(h *ZarfHost) bool { return h.Role == "worker" })
		require.Same(t, worker, found)
	})

	t.Run("no match returns nil", func(t *testing.T) {
		found := hosts.Find(func(h *ZarfHost) bool { return h.Role == "missing" })
		require.Nil(t, found)
	})
}

func TestHostsFilter(t *testing.T) {
	controller := &ZarfHost{Role: "controller"}
	worker1 := &ZarfHost{Role: "worker"}
	worker2 := &ZarfHost{Role: "worker"}
	hosts := ZarfHosts{controller, worker1, worker2}

	filtered := hosts.Filter(func(h *ZarfHost) bool { return h.Role == "worker" })
	require.Equal(t, ZarfHosts{worker1, worker2}, filtered)

	t.Run("no matches returns empty, not nil", func(t *testing.T) {
		filtered := hosts.Filter(func(_ *ZarfHost) bool { return false })
		require.NotNil(t, filtered)
		require.Empty(t, filtered)
	})
}

func TestHostsWithRole(t *testing.T) {
	controller := &ZarfHost{Role: RoleController}
	worker := &ZarfHost{Role: RoleWorker}
	hosts := ZarfHosts{controller, worker}

	require.Equal(t, ZarfHosts{worker}, hosts.WithRole(RoleWorker))
	require.Equal(t, ZarfHosts{controller}, hosts.WithRole(RoleController))
}

func TestHostsControllers(t *testing.T) {
	controller := &ZarfHost{Role: RoleController}
	controllerWorker := &ZarfHost{Role: RoleControllerWorker}
	single := &ZarfHost{Role: RoleSingle}
	worker := &ZarfHost{Role: RoleWorker}
	hosts := ZarfHosts{controller, controllerWorker, single, worker}

	require.Equal(t, ZarfHosts{controller, controllerWorker, single}, hosts.Controllers())
}

func TestHostsWorkers(t *testing.T) {
	controller := &ZarfHost{Role: RoleController}
	worker := &ZarfHost{Role: RoleWorker}
	hosts := ZarfHosts{controller, worker}

	require.Equal(t, ZarfHosts{worker}, hosts.Workers())
}

func TestHostsParallelEach(t *testing.T) {
	hosts := ZarfHosts{
		&ZarfHost{Role: "controller"},
		&ZarfHost{Role: "worker"},
	}

	t.Run("success", func(t *testing.T) {
		var count atomic.Int32
		fn := func(_ context.Context, _ *ZarfHost) error {
			count.Add(1)
			return nil
		}
		err := hosts.ParallelEach(context.Background(), fn)
		require.NoError(t, err)
		require.EqualValues(t, 2, count.Load())
	})

	t.Run("collects errors from every host", func(t *testing.T) {
		fn := func(_ context.Context, h *ZarfHost) error {
			return fmt.Errorf("failed on %s", h.Role)
		}
		err := hosts.ParallelEach(context.Background(), fn)
		require.Error(t, err)
		require.ErrorContains(t, err, "failed on 2 hosts")
		require.ErrorContains(t, err, "controller")
		require.ErrorContains(t, err, "worker")
	})

	t.Run("context canceled before filter runs", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		fn := func(_ context.Context, _ *ZarfHost) error {
			return nil
		}
		err := hosts.ParallelEach(ctx, fn)
		require.Error(t, err)
		require.ErrorContains(t, err, "error from context")
	})

	t.Run("runs filters in order across all hosts", func(t *testing.T) {
		var mu sync.Mutex
		var order []string

		first := func(_ context.Context, h *ZarfHost) error {
			mu.Lock()
			order = append(order, "first:"+h.Role)
			mu.Unlock()
			return nil
		}
		second := func(_ context.Context, h *ZarfHost) error {
			mu.Lock()
			order = append(order, "second:"+h.Role)
			mu.Unlock()
			return nil
		}

		require.NoError(t, hosts.ParallelEach(context.Background(), first, second))
		require.Len(t, order, 4)
		// Both "first" entries must precede both "second" entries, since each filter
		// completes across all hosts before the next filter starts.
		require.Contains(t, order[:2], "first:controller")
		require.Contains(t, order[:2], "first:worker")
		require.Contains(t, order[2:], "second:controller")
		require.Contains(t, order[2:], "second:worker")
	})
}

func TestHostsBatchedParallelEach(t *testing.T) {
	hosts := ZarfHosts{
		&ZarfHost{Role: "h1"},
		&ZarfHost{Role: "h2"},
		&ZarfHost{Role: "h3"},
		&ZarfHost{Role: "h4"},
		&ZarfHost{Role: "h5"},
	}

	t.Run("processes all hosts across batches", func(t *testing.T) {
		var mu sync.Mutex
		var seen []string

		fn := func(_ context.Context, h *ZarfHost) error {
			mu.Lock()
			seen = append(seen, h.Role)
			mu.Unlock()
			return nil
		}

		err := hosts.BatchedParallelEach(context.Background(), 2, fn)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"h1", "h2", "h3", "h4", "h5"}, seen)
	})

	t.Run("stops at first batch error", func(t *testing.T) {
		fn := func(_ context.Context, h *ZarfHost) error {
			if h.Role == "h1" {
				return errors.New("boom")
			}
			return nil
		}
		err := hosts.BatchedParallelEach(context.Background(), 1, fn)
		require.Error(t, err)
		require.ErrorContains(t, err, "boom")
	})

	t.Run("context canceled before any batch runs", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		fn := func(_ context.Context, _ *ZarfHost) error {
			return nil
		}
		err := hosts.BatchedParallelEach(ctx, 2, fn)
		require.Error(t, err)
		require.ErrorContains(t, err, "error from context")
	})

	t.Run("empty hosts is a no-op", func(t *testing.T) {
		var hosts ZarfHosts
		fn := func(_ context.Context, _ *ZarfHost) error {
			t.Fatal("filter should not be called")
			return nil
		}
		require.NoError(t, hosts.BatchedParallelEach(context.Background(), 2, fn))
	})
}

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
