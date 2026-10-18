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

// Package actions runs the onCreate actions in a distro package.
//
// Derived from zarf's src/pkg/packager/actions and the network half of
// src/pkg/wait. zarf's wait package serves both cluster and network waits from
// one file, so importing it for a network probe linked k8s.io/kubectl,
// k8s.io/cli-runtime and sigs.k8s.io/kustomize - about 5.9MB of a binary that
// never has a cluster to wait on, because cargoship runs actions only at
// package create time. See docs/agent/choice-actions-fork.md.
package actions

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// ForNetwork waits for a network endpoint to respond.
func ForNetwork(ctx context.Context, protocol, address, condition string, timeout time.Duration) error {
	waitInterval := time.Second
	return forNetwork(ctx, protocol, address, condition, timeout, waitInterval)
}

var errNetworkNotReady = errors.New("network endpoint not ready")

func forNetwork(ctx context.Context, protocol string, address string, condition string, timeout time.Duration, waitInterval time.Duration) error {
	return waitForNetwork(ctx, protocol, address, condition, timeout, waitInterval, probeNetwork)
}

func waitForNetwork(
	ctx context.Context,
	protocol string,
	address string,
	condition string,
	timeout time.Duration,
	waitInterval time.Duration,
	probe func(context.Context, string, string, string, time.Duration) (bool, error),
) error {
	l := logger.From(ctx)
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	condition = strings.ToLower(condition)
	if condition == "" {
		condition = "success"
	}

	err := retry.Do(func() error {
		ok, err := probe(timeoutCtx, protocol, address, condition, waitInterval)
		if err != nil {
			l.Debug(err.Error())
			return err
		}
		if !ok {
			return errNetworkNotReady
		}
		return nil
	},
		retry.Attempts(0),
		retry.Delay(waitInterval),
		retry.DelayType(retry.FixedDelay),
		retry.LastErrorOnly(true),
		retry.Context(timeoutCtx),
		retry.OnRetry(func(_ uint, err error) {
			if !errors.Is(err, errNetworkNotReady) {
				l.Debug("retrying network wait", "err", err)
			}
		}),
	)
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		if ctx.Err() != nil {
			return fmt.Errorf("wait cancelled: %w", ctx.Err())
		}
		return errors.New("wait timed out")
	}
	return err
}

func probeNetwork(ctx context.Context, protocol string, address string, condition string, waitInterval time.Duration) (bool, error) {
	switch protocol {
	case "http", "https":
		return probeHTTP(ctx, protocol, address, condition, waitInterval)
	default:
		return probeTCP(ctx, protocol, address)
	}
}

func probeHTTP(ctx context.Context, protocol string, address string, condition string, waitInterval time.Duration) (ok bool, err error) {
	expectedStatusCode := 0
	if condition != "success" {
		var err error
		expectedStatusCode, err = strconv.Atoi(condition)
		if err != nil {
			return false, retry.Unrecoverable(fmt.Errorf("http status code %s is not an integer: %w", condition, err))
		}
		if http.StatusText(expectedStatusCode) == "" {
			return false, retry.Unrecoverable(errors.New("http status code is unknown"))
		}
	}

	// Create an HTTP client with a per-request timeout that is slightly shorter than our wait-interval to prevent
	// hanging on slow or unresponsive servers.
	requestTimeout := waitInterval - (time.Millisecond * 5)
	if requestTimeout < time.Millisecond {
		requestTimeout = time.Millisecond
	}
	httpClient := &http.Client{
		Timeout: requestTimeout,
	}

	url := fmt.Sprintf("%s://%s", protocol, address)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	// Default to checking for a 2xx response.
	if condition == "success" {
		return resp.StatusCode >= 200 && resp.StatusCode <= 299, nil
	}

	return resp.StatusCode == expectedStatusCode, nil
}

func probeTCP(ctx context.Context, protocol string, address string) (bool, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, protocol, address)
	if err != nil {
		return false, err
	}
	err = conn.Close()
	if err != nil {
		return false, err
	}
	return true, nil
}
