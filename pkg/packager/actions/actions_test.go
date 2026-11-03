// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
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

package actions

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	zarfapi "github.com/zarf-dev/zarf/src/api"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/variables"
)

func testVariableConfig(ctx context.Context) *variables.VariableConfig {
	prompt := func(v zarfapi.InteractiveVariable) (string, error) {
		return v.Default, nil
	}
	return variables.New("zarf", prompt, logger.From(ctx))
}

func seconds(n int) *int { return &n }

// TestRunWaitClusterActionIsRejected pins the one behaviour this fork deliberately
// drops. Upstream would wait against whatever kubeconfig the build machine has, which
// is not the cluster the package is applied to, so this fails naming the resource
// instead of blocking for the action's timeout.
func TestRunWaitClusterActionIsRejected(t *testing.T) {
	ctx := context.Background()
	action := zarfapi.Action{
		Description:     "wait for the api server",
		MaxTotalSeconds: seconds(1),
		Wait: &zarfapi.ActionWait{
			Cluster: &zarfapi.ActionWaitCluster{
				Kind: "deployment",
				Name: "coredns",
			},
		},
	}

	start := time.Now()
	err := Run(ctx, t.TempDir(), []zarfapi.Action{action}, RunOptions{
		VariableConfig: testVariableConfig(ctx),
	})
	if err == nil {
		t.Fatal("Run() returned no error for a wait.cluster action")
	}
	for _, want := range []string{"wait.cluster", "deployment", "coredns", "wait.network"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	// It has to fail rather than wait out the clock, which is the whole point.
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("Run() took %s, so it waited rather than refusing", elapsed)
	}
}

// TestRunWaitNetworkAction covers the half of zarf's wait package this fork kept.
func TestRunWaitNetworkAction(t *testing.T) {
	ctx := context.Background()

	t.Run("http waits for the expected status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		err := Run(ctx, t.TempDir(), []zarfapi.Action{{
			MaxTotalSeconds: seconds(10),
			Wait: &zarfapi.ActionWait{
				Network: &zarfapi.ActionWaitNetwork{
					Protocol: "http",
					Address:  strings.TrimPrefix(srv.URL, "http://"),
				},
			},
		}}, RunOptions{VariableConfig: testVariableConfig(ctx)})
		if err != nil {
			t.Fatalf("Run() unexpected error: %v", err)
		}
	})

	t.Run("tcp waits for a listener", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to listen: %v", err)
		}
		t.Cleanup(func() {
			if err := ln.Close(); err != nil {
				t.Errorf("failed to close the listener: %v", err)
			}
		})

		err = Run(ctx, t.TempDir(), []zarfapi.Action{{
			MaxTotalSeconds: seconds(10),
			Wait: &zarfapi.ActionWait{
				Network: &zarfapi.ActionWaitNetwork{
					Protocol: "tcp",
					Address:  ln.Addr().String(),
				},
			},
		}}, RunOptions{VariableConfig: testVariableConfig(ctx)})
		if err != nil {
			t.Fatalf("Run() unexpected error: %v", err)
		}
	})

	t.Run("times out when nothing answers", func(t *testing.T) {
		// Port 1 on loopback: reserved, and nothing in CI binds it.
		err := Run(ctx, t.TempDir(), []zarfapi.Action{{
			MaxTotalSeconds: seconds(1),
			Wait: &zarfapi.ActionWait{
				Network: &zarfapi.ActionWaitNetwork{
					Protocol: "tcp",
					Address:  "127.0.0.1:1",
				},
			},
		}}, RunOptions{VariableConfig: testVariableConfig(ctx)})
		if err == nil {
			t.Fatal("Run() returned no error for an address nothing listens on")
		}
	})
}

// TestRunRequiresAVariableConfig covers the nil fallback this fork replaced: upstream
// built one out of an internal package, and a per-call config forgets the variables an
// action set.
func TestRunRequiresAVariableConfig(t *testing.T) {
	err := Run(context.Background(), t.TempDir(), []zarfapi.Action{{Cmd: "true"}}, RunOptions{})
	if err == nil {
		t.Fatal("Run() returned no error without a VariableConfig")
	}
	if !strings.Contains(err.Error(), "VariableConfig") {
		t.Errorf("error %q does not name what is missing", err)
	}
}

// TestRunCmdAction is the baseline: the exec path the fork did not touch still runs.
func TestRunCmdAction(t *testing.T) {
	ctx := context.Background()
	err := Run(ctx, t.TempDir(), []zarfapi.Action{{
		Cmd:             "echo forked",
		MaxTotalSeconds: seconds(30),
	}}, RunOptions{VariableConfig: testVariableConfig(ctx)})
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
}
