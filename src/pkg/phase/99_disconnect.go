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

	v1alpha1 "github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
)

// Disconnect disconnects from the hosts
type Disconnect struct {
	GenericPhase
}

// Title for the phase
func (p *Disconnect) Title() string {
	return "Disconnect from hosts"
}

// DryRun cleans up the temporary binary from the hosts
func (p *Disconnect) DryRun() error {
	_ = p.manager.Config.Spec.Hosts.ParallelEach(context.Background(), func(_ context.Context, h *v1alpha1.ZarfHost) error {
		if len(h.Metadata.BinaryTempFile) > 0 {
			for _, f := range h.Metadata.BinaryTempFile {
				if h.FileExist(f) {
					_ = h.Configurer.DeleteFile(h, f)
				}
			}
		}
		h.Metadata.BinaryTempFile = []string{}
		return nil
	})

	return p.Run(context.TODO())
}

// Run the phase
func (p *Disconnect) Run(ctx context.Context) error {
	return p.manager.Config.Spec.Hosts.ParallelEach(ctx, func(_ context.Context, h *v1alpha1.ZarfHost) error {
		h.Disconnect()
		return nil
	})
}
