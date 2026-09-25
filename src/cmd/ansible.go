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

package cmd

import (
	"context"

	"github.com/colonel-byte/cargoship/internal/ansiblemod"
)

// AnsibleModule runs the binary as an Ansible module when it was invoked as one, and reports
// whether it was.
//
// It lives here rather than in main because src/internal is only importable from inside src/, and
// because this is where the argument vector a module renders is executed. The caller exits with
// the status returned; a module always exits 0 and reports failure inside its JSON result.
func AnsibleModule(ctx context.Context, argv []string) (int, bool) {
	if len(argv) == 0 {
		return 0, false
	}
	name, ok := ansiblemod.ModuleName(argv[0])
	if !ok {
		return 0, false
	}
	return ansiblemod.Run(ctx, name, argv, ExecuteArgs), true
}
