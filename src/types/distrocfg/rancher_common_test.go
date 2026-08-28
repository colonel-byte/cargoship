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

package distrocfg

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/k0sproject/dig"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

func testLoggerContext() (context.Context, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	l := slog.New(slog.NewTextHandler(buf, nil))
	return logger.WithContext(context.Background(), l), buf
}

func TestValidateEngineConfigKnownVersionDropsUnknownKeys(t *testing.T) {
	ctx, buf := testLoggerContext()
	d := &RancherCommon{Common: Common{ID: "k3s"}}

	cfg := dig.Mapping{
		"cluster-cidr":  []string{"10.42.0.0/16"},
		"node-name":     "host-1",
		"totally-typod": "value",
	}

	d.validateEngineConfig(ctx, "1.35.3-k3s1", true, cfg)

	require.Contains(t, cfg, "cluster-cidr")
	require.Contains(t, cfg, "node-name")
	require.NotContains(t, cfg, "totally-typod")

	out := buf.String()
	require.NotContains(t, out, `key=cluster-cidr`)
	require.NotContains(t, out, `key=node-name`)
	require.Contains(t, out, "engine config key not recognized")
	require.Contains(t, out, `key=totally-typod`)
}

func TestValidateEngineConfigUnknownVersionBlindlyKeepsAllKeys(t *testing.T) {
	ctx, buf := testLoggerContext()
	d := &RancherCommon{Common: Common{ID: "k3s"}}

	cfg := dig.Mapping{"anything": "goes", "totally-typod": "value"}

	d.validateEngineConfig(ctx, "9.99.99-k3s1", true, cfg)

	require.Equal(t, dig.Mapping{"anything": "goes", "totally-typod": "value"}, cfg)

	out := buf.String()
	require.Contains(t, out, "no generated engine config schema for this distro/version")
	require.NotContains(t, out, "engine config key not recognized")
}

func TestValidateEngineConfigChecksAgentVsServerTarget(t *testing.T) {
	// "cluster-cidr" is a server-only k3s flag: valid for a controller, unrecognized (and
	// dropped) on an agent-only node.
	serverCfg := dig.Mapping{"cluster-cidr": []string{"10.42.0.0/16"}}
	serverCtx, serverBuf := testLoggerContext()
	d := &RancherCommon{Common: Common{ID: "k3s"}}
	d.validateEngineConfig(serverCtx, "1.35.3-k3s1", true, serverCfg)
	require.Contains(t, serverCfg, "cluster-cidr")
	require.NotContains(t, serverBuf.String(), "engine config key not recognized")

	agentCfg := dig.Mapping{"cluster-cidr": []string{"10.42.0.0/16"}}
	agentCtx, agentBuf := testLoggerContext()
	d.validateEngineConfig(agentCtx, "1.35.3-k3s1", false, agentCfg)
	require.NotContains(t, agentCfg, "cluster-cidr")
	require.Contains(t, agentBuf.String(), "engine config key not recognized")
	require.Contains(t, agentBuf.String(), `key=cluster-cidr`)
}
