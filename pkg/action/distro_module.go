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

package action

import (
	"errors"
	"fmt"

	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/colonel-byte/cargoship/types/distrocfg/registry"
)

// ErrNoDistroSelected is returned when an action is built with no distro at all, rather than with
// one no module claims.
var ErrNoDistroSelected = errors.New("no distro selected: pass --distro, or set distro.type in the cargoship config file")

// distroModule builds the distro module named by id.
//
// Every action needs this and each used to inline it, which is how three of them came to drop the
// error on the floor and hand their caller a nil action to call a method on. Both failures belong
// to the lookup rather than to any one action, so they are reported here.
//
// An empty id is reported as the missing setting it is rather than as a lookup that failed. A
// command invoked with no --distro, outside a directory holding a cargoship config file and with
// no CARGOSHIP_CONFIG set, reaches here with "", and a module-not-found error does not tell an
// operator what to pass.
func distroModule(id string) (distrocfg.Distro, error) {
	if id == "" {
		return nil, ErrNoDistroSelected
	}

	builder, err := registry.GetDistroModuleBuilder(id)
	if err != nil {
		return nil, fmt.Errorf("no distro module for %q: %w", id, err)
	}

	d, ok := builder().(distrocfg.Distro)
	if !ok {
		return nil, fmt.Errorf("the distro module for %q does not implement the distro interface", id)
	}

	return d, nil
}
