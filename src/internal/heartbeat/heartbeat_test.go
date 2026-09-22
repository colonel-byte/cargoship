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

package heartbeat_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/src/internal/heartbeat"
	"github.com/stretchr/testify/require"
)

func TestHeartbeat(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "status.json")
	heartbeat.Init(tmpFile)

	heartbeat.Update("Test Phase", 1, 10, "running", nil)

	data, err := os.ReadFile(tmpFile)
	require.NoError(t, err)

	var st heartbeat.Status
	require.NoError(t, json.Unmarshal(data, &st))
	require.Equal(t, "Test Phase", st.Phase)
	require.Equal(t, 1, st.Index)
	require.Equal(t, 10, st.Total)
	require.Equal(t, "running", st.Status)
	require.Empty(t, st.Error)

	heartbeat.Update("Test Phase", 1, 10, "failed", errors.New("boom"))
	data, err = os.ReadFile(tmpFile)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &st))
	require.Equal(t, "failed", st.Status)
	require.Equal(t, "boom", st.Error)

	heartbeat.Clear()
	_, err = os.Stat(tmpFile)
	require.True(t, os.IsNotExist(err))
}
