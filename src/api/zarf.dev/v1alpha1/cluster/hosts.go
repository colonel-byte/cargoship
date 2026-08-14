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
	"fmt"
	"strings"
	"sync"
)

// ZarfHosts is an ordered list of hosts that cargoship manages together.
type ZarfHosts []*ZarfHost

// First returns the first host. It returns nil if there are no hosts.
func (hosts ZarfHosts) First() *ZarfHost {
	if len(hosts) == 0 {
		return nil
	}
	return (hosts)[0]
}

// Last returns the last host. It returns nil if there are no hosts.
func (hosts ZarfHosts) Last() *ZarfHost {
	c := len(hosts) - 1

	if c < 0 {
		return nil
	}

	return hosts[c]
}

// Find returns the first host for which filter returns true. It returns nil if no host matches.
func (hosts ZarfHosts) Find(filter func(h *ZarfHost) bool) *ZarfHost {
	for _, h := range hosts {
		if filter(h) {
			return (h)
		}
	}
	return nil
}

// Filter returns the hosts for which filter returns true.
func (hosts ZarfHosts) Filter(filter func(h *ZarfHost) bool) ZarfHosts {
	result := make(ZarfHosts, 0, len(hosts))

	for _, h := range hosts {
		if filter(h) {
			result = append(result, h)
		}
	}

	return result
}

// WithRole returns the hosts that have the given role.
func (hosts ZarfHosts) WithRole(s string) ZarfHosts {
	return hosts.Filter(func(h *ZarfHost) bool {
		return h.Role == s
	})
}

// Controllers returns the hosts that act as a controller. This includes hosts with role controller, controller+worker, or single.
func (hosts ZarfHosts) Controllers() ZarfHosts {
	return hosts.Filter(func(h *ZarfHost) bool { return h.IsController() })
}

// Workers returns the hosts with role worker.
func (hosts ZarfHosts) Workers() ZarfHosts {
	return hosts.WithRole(RoleWorker)
}

// Each runs each filter on every host, in the order given. It stops and returns the error if ctx is canceled or a filter returns an error.
func (hosts ZarfHosts) Each(ctx context.Context, filters ...func(context.Context, *ZarfHost) error) error {
	for _, filter := range filters {
		for _, h := range hosts {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("error from context: %w", err)
			}
			if err := filter(ctx, h); err != nil {
				return err
			}
		}
	}

	return nil
}

// ParallelEach runs each filter on every host in parallel. It runs the filters in the order given, completing one filter across all hosts before it starts the next.
// It collects every error and returns them combined.
func (hosts ZarfHosts) ParallelEach(ctx context.Context, filters ...func(context.Context, *ZarfHost) error) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []string

	for _, filter := range filters {
		for _, h := range hosts {
			wg.Add(1)
			go func(h *ZarfHost) {
				defer wg.Done()
				if err := ctx.Err(); err != nil {
					mu.Lock()
					errors = append(errors, fmt.Sprintf("error from context: %v", err))
					mu.Unlock()
					return
				}
				if err := filter(ctx, h); err != nil {
					mu.Lock()
					errors = append(errors, fmt.Sprintf("%s: %s", h.String(), err.Error()))
					mu.Unlock()
				}
			}(h)
		}
		wg.Wait()
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed on %d hosts:\n - %s", len(errors), strings.Join(errors, "\n - "))
	}

	return nil
}

// BatchedParallelEach runs each filter on every host in parallel, in groups of batchSize hosts. It completes one group before it starts the next.
// It stops and returns the error if ctx is canceled or a group returns an error.
func (hosts ZarfHosts) BatchedParallelEach(ctx context.Context, batchSize int, filter ...func(context.Context, *ZarfHost) error) error {
	for i := 0; i < len(hosts); i += batchSize {
		end := min(i+batchSize, len(hosts))
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("error from context: %w", err)
		}
		if err := hosts[i:end].ParallelEach(ctx, filter...); err != nil {
			return err
		}
	}

	return nil
}
