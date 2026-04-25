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

package phase

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/types/os"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// ValidateHosts performs remote OS detection
type ValidateHosts struct {
	GenericPhase
	hncount          map[string]int
	privateaddrcount map[string]int
}

// Title for the phase
func (p *ValidateHosts) Title() string {
	return "Validate hosts"
}

// Run the phase
func (p *ValidateHosts) Run(ctx context.Context) error {
	p.hncount = make(map[string]int, len(p.manager.Config.Spec.Hosts))
	p.privateaddrcount = make(map[string]int, len(p.manager.Config.Spec.Hosts))

	for _, h := range p.manager.Config.Spec.Hosts {
		p.hncount[h.Metadata.Hostname]++
		if h.PrivateAddress != "" {
			p.privateaddrcount[h.PrivateAddress]++
		}
	}

	err := p.parallelDo(
		ctx,
		p.manager.Config.Spec.Hosts,
		p.validateUniqueHostname,
		p.validateUniquePrivateAddress,
		p.validateSudo,
		p.validateConfigurer,
	)
	if err != nil {
		return err
	}

	return p.validateClockSkew(ctx)
}

func (p *ValidateHosts) validateUniqueHostname(_ context.Context, h *cluster.ZarfHost) error {
	if p.hncount[h.Metadata.Hostname] > 1 {
		return fmt.Errorf("hostname is not unique: %s", h.Metadata.Hostname)
	}

	return nil
}

func (p *ValidateHosts) validateUniquePrivateAddress(_ context.Context, h *cluster.ZarfHost) error {
	if p.privateaddrcount[h.PrivateAddress] > 1 {
		return fmt.Errorf("privateAddress %q is not unique: %s", h.PrivateAddress, h.Metadata.Hostname)
	}

	return nil
}

func (p *ValidateHosts) validateSudo(_ context.Context, h *cluster.ZarfHost) error {
	return h.Configurer.CheckPrivilege(h)
}

func (p *ValidateHosts) validateConfigurer(_ context.Context, h *cluster.ZarfHost) error {
	validator, ok := h.Configurer.(os.HostValidator)
	if !ok {
		return nil
	}

	return validator.ValidateHost(h)
}

const maxSkew = 30 * time.Second

func (p *ValidateHosts) validateClockSkew(ctx context.Context) error {
	logger.From(ctx).Info("validating clock skew")
	skews := make(map[*cluster.ZarfHost]time.Duration, len(p.manager.Config.Spec.Hosts))
	var skewValues []time.Duration
	var mu sync.Mutex

	// Collect skews relative to local time
	err := p.parallelDo(ctx, p.manager.Config.Spec.Hosts, func(_ context.Context, h *cluster.ZarfHost) error {
		remote, err := h.Configurer.SystemTime(h)
		if err != nil {
			return fmt.Errorf("failed to get time from %s: %w", h, err)
		}
		skew := time.Now().UTC().Sub(remote).Round(time.Second)
		mu.Lock()
		skews[h] = skew
		skewValues = append(skewValues, skew)
		mu.Unlock()
		return nil
	})
	if err != nil {
		return err
	}

	// Sort skews to find the median
	slices.Sort(skewValues)
	median := skewValues[len(skewValues)/2]

	// Check if any skew exceeds the maxSkew relative to the median
	var foundExceeding int
	for h, skew := range skews {
		deviation := (skew - median).Abs()
		if deviation > maxSkew {
			logger.From(ctx).Error(fmt.Sprintf("clock skew exceeds the maximum of %.0f seconds", maxSkew.Seconds()), "host", h, "skew", deviation.Seconds())
			foundExceeding++
		}
	}

	if foundExceeding > 0 {
		return fmt.Errorf("clock skew exceeds the maximum on %d hosts", foundExceeding)
	}

	return nil
}
