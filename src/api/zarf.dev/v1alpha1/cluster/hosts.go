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
	"fmt"
	"strings"
	"sync"
)

// ZarfHosts are the hosts that will be managed
type ZarfHosts []*ZarfHost

// First returns the first host
func (hosts ZarfHosts) First() *ZarfHost {
	if len(hosts) == 0 {
		return nil
	}
	return (hosts)[0]
}

// Last returns the last host
func (hosts ZarfHosts) Last() *ZarfHost {
	c := len(hosts) - 1

	if c < 0 {
		return nil
	}

	return hosts[c]
}

// Find returns the first matching Host. The finder function should return true for a Host matching the criteria.
func (hosts ZarfHosts) Find(filter func(h *ZarfHost) bool) *ZarfHost {
	for _, h := range hosts {
		if filter(h) {
			return (h)
		}
	}
	return nil
}

// Filter returns a filtered list of Hosts. The filter function should return true for hosts matching the criteria.
func (hosts ZarfHosts) Filter(filter func(h *ZarfHost) bool) ZarfHosts {
	result := make(ZarfHosts, 0, len(hosts))

	for _, h := range hosts {
		if filter(h) {
			result = append(result, h)
		}
	}

	return result
}

// WithRole returns a ltered list of Hosts that have the given role
func (hosts ZarfHosts) WithRole(s string) ZarfHosts {
	return hosts.Filter(func(h *ZarfHost) bool {
		return h.Role == s
	})
}

// Controllers returns hosts with the role "controller"
func (hosts ZarfHosts) Controllers() ZarfHosts {
	return hosts.Filter(func(h *ZarfHost) bool { return h.IsController() })
}

// Workers returns hosts with the role "worker"
func (hosts ZarfHosts) Workers() ZarfHosts {
	return hosts.WithRole(RoleWorker)
}

// Each runs a function (or multiple functions chained) on every Host.
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

// ParallelEach runs a function (or multiple functions chained) on every Host parallelly.
// Any errors will be concatenated and returned.
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

// BatchedParallelEach runs a function (or multiple functions chained) on every Host parallelly in groups of batchSize hosts.
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
