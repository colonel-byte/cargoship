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

package test

import (
	"fmt"
	"testing"
	"time"
)

func TestZZSpikeHold(_ *testing.T) {
	fmt.Println("SPIKE_CLUSTER_CONFIG_PATH=" + e2e.ClusterConfigPath)
	fmt.Println("SPIKE_CARGO_BIN_PATH=" + e2e.CargoBinPath)
	time.Sleep(20 * time.Minute)
}
