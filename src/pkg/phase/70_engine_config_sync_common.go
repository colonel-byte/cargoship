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
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/internal/clustercfg"
	"github.com/colonel-byte/cargoship/src/pkg/node"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// EngineConfigSyncHosts phase state
type EngineConfigSyncHosts struct {
	GenericPhase
	Distro distrocfg.Distro
	// VaultPassword decrypts Ansible Vault-encrypted registry credentials.
	VaultPassword string
	desired       map[string][]byte
	service       string
	hosts         cluster.ZarfHosts
	leader        *cluster.ZarfHost
}

// ShouldRun is true when there are hosts to sync
func (p *EngineConfigSyncHosts) ShouldRun() bool {
	return len(p.hosts) > 0
}

func (p *EngineConfigSyncHosts) prepareLeader() error {
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.ServiceIsRunning(h, p.Distro.GetControllerService()) && h.IsController()
	})
	if len(control) == 0 {
		return ErrNoControllers
	}
	p.leader = control[0]
	return nil
}

func (p *EngineConfigSyncHosts) loadDesiredConfig(c *cluster.ZarfCluster, dis distro.ZarfDistro) error {
	if err := clustercfg.DecryptRegistryAuth(c, p.VaultPassword); err != nil {
		return err
	}
	run := cluster.ZarfRuntimeMeta{Registries: c.Spec.Config.Registries}

	desired, err := p.Distro.DesiredFiles(cluster.ZarfHost{}, run, dis)
	if err != nil {
		return err
	}
	p.desired = desired
	return nil
}

func (p *EngineConfigSyncHosts) needsUpdate(h *cluster.ZarfHost) bool {
	for path, want := range p.desired {
		if !h.FileExist(path) {
			return true
		}
		current, err := h.ReadFile(path)
		if err != nil || current != string(want) {
			return true
		}
	}
	return false
}

func (p *EngineConfigSyncHosts) writeFiles(_ context.Context, h *cluster.ZarfHost) error {
	for path, content := range p.desired {
		if err := h.WriteFile(path, string(content), "0600"); err != nil {
			return err
		}
	}
	return nil
}

func (p *EngineConfigSyncHosts) drainNode(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("draining nodes", "node", h)
	return p.manager.RetryTimeout(ctx, func(_ context.Context) error {
		return p.leader.Exec(p.Distro.KubectlCmdf(*p.leader, p.Distro.DataDirPath(), drainNode, h.Configurer.Hostname(h)), exec.Sudo(p.leader))
	})
}

func (p *EngineConfigSyncHosts) startService(ctx context.Context, h *cluster.ZarfHost) error {
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

func (p *EngineConfigSyncHosts) waitForNodeReady(ctx context.Context, h *cluster.ZarfHost) error {
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

func (p *EngineConfigSyncHosts) uncordonNode(_ context.Context, h *cluster.ZarfHost) error {
	return p.leader.Exec(p.Distro.KubectlCmdf(*p.leader, p.Distro.DataDirPath(), uncordonNode, h.Configurer.Hostname(h)), exec.Sudo(p.leader))
}
