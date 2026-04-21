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

package node

import (
	"context"
	"fmt"

	"github.com/colonel-byte/mare/src/api/zarf.dev/v1alpha1/cluster"
)

type retryFunc func(context.Context) error

// ServiceRunningFunc returns a function that returns an error until the service is running on the host
func ServiceRunningFunc(h *cluster.ZarfHost, service string) retryFunc {
	return func(_ context.Context) error {
		if !h.Configurer.ServiceIsRunning(h, service) {
			return fmt.Errorf("service %s is not running", service)
		}
		return nil
	}
}

// HTTPStatus returns a function that returns an error unless the expected status code is returned for a HTTP get to the url
func HTTPStatusFunc(h *cluster.ZarfHost, url string, expected ...int) retryFunc {
	return func(_ context.Context) error {
		return h.CheckHTTPStatus(url, expected...)
	}
}

// ServiceStoppedFunc returns a function that returns an error if the service is not running on the host
func ServiceStoppedFunc(h *cluster.ZarfHost, service string) retryFunc {
	return func(_ context.Context) error {
		if h.Configurer.ServiceIsRunning(h, service) {
			return fmt.Errorf("service %s is still running", service)
		}
		return nil
	}
}
