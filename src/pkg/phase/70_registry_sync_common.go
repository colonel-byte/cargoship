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
	"errors"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/internal/clustercfg"
	"github.com/colonel-byte/cargoship/src/pkg/node"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// RegistrySyncHosts phase state
type RegistrySyncHosts struct {
	GenericPhase
	Distro distrocfg.Distro
	// VaultPassword decrypts Ansible Vault-encrypted registry credentials.
	VaultPassword string
	registries    []cluster.ZarfClusterRegistries
	desired       []byte
	service       string
	hosts         cluster.ZarfHosts
	leader        *cluster.ZarfHost
}

// ShouldRun is true when there are hosts to sync
func (p *RegistrySyncHosts) ShouldRun() bool {
	return len(p.hosts) > 0
}

func (p *RegistrySyncHosts) prepareLeader() error {
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.ServiceIsRunning(h, p.Distro.GetControllerService()) && h.IsController()
	})
	if len(control) == 0 {
		return ErrNoControllers
	}
	p.leader = control[0]
	return nil
}

func (p *RegistrySyncHosts) loadDesiredConfig(c *cluster.ZarfCluster) error {
	if err := clustercfg.DecryptRegistryAuth(c, p.VaultPassword); err != nil {
		return err
	}
	p.registries = c.Spec.Config.Registries

	desired, err := p.Distro.RenderRegistriesConfig(p.registries)
	if err != nil {
		return err
	}
	p.desired = desired
	return nil
}

func (p *RegistrySyncHosts) needsUpdate(h *cluster.ZarfHost) bool {
	path := p.Distro.RegistriesConfigPath()
	if !h.FileExist(path) {
		return len(p.registries) > 0
	}
	current, err := h.ReadFile(path)
	if err != nil {
		return true
	}
	return current != string(p.desired)
}

func (p *RegistrySyncHosts) writeRegistries(ctx context.Context, h *cluster.ZarfHost) error {
	return p.Distro.ConfigureRegistries(ctx, *h, p.registries)
}

func (p *RegistrySyncHosts) drainNode(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("draining nodes", "node", h)
	return p.manager.RetryTimeout(ctx, func(_ context.Context) error {
		return p.leader.Exec(p.Distro.KubectlCmdf(*p.leader, p.Distro.DataDirPath(), drainNode, h.Configurer.Hostname(h)), exec.Sudo(p.leader))
	})
}

func (p *RegistrySyncHosts) startService(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("waiting for the service to start", "service", p.service, "host", h)

	startedAt := time.Now()
	go func() {
		err := h.Configurer.StartService(h, p.service)
		if err != nil {
			logger.From(ctx).Warn("failed to start", "service", p.service, "host", h)
		}
	}()

	if err := p.manager.RetryTimeout(ctx, node.ServiceRunningFunc(h, p.service)); err != nil {
		return p.captureServiceLogsOnFailure(ctx, h, p.service, startedAt, err)
	}

	return h.Configurer.EnableService(h, p.service)
}

func (p *RegistrySyncHosts) waitForNodeReady(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("waiting for the node to be in a ready state", "host", h)

	return p.manager.RetryTimeout(ctx, func(_ context.Context) error {
		out, err := p.leader.ExecOutput(p.Distro.KubectlCmdf(*p.leader, p.Distro.DataDirPath(), readyNode, h.Configurer.Hostname(h)), exec.Sudo(p.leader))
		if err != nil {
			return err
		}
		if strings.ToLower(out) != "true" {
			return errors.New("node not ready")
		}
		return nil
	})
}

func (p *RegistrySyncHosts) uncordonNode(_ context.Context, h *cluster.ZarfHost) error {
	return p.leader.Exec(p.Distro.KubectlCmdf(*p.leader, p.Distro.DataDirPath(), uncordonNode, h.Configurer.Hostname(h)), exec.Sudo(p.leader))
}
