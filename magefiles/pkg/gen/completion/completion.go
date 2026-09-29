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

package completion

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/colonel-byte/cargoship/cmd"
)

// Generate generates shell completion scripts for cargoship into targetDir.
func Generate(targetDir string) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("creating completion dir: %w", err)
	}

	shells := []struct {
		name     string
		filename string
		gen      func(path string) error
	}{
		{
			name:     "bash",
			filename: "cargoship.bash",
			gen: func(path string) error {
				f, err := os.Create(path)
				if err != nil {
					return err
				}
				defer f.Close()
				return cmd.NewCargoshipCommand().GenBashCompletionV2(f, true)
			},
		},
		{
			name:     "zsh",
			filename: "cargoship.zsh",
			gen: func(path string) error {
				f, err := os.Create(path)
				if err != nil {
					return err
				}
				defer f.Close()
				return cmd.NewCargoshipCommand().GenZshCompletion(f)
			},
		},
		{
			name:     "fish",
			filename: "cargoship.fish",
			gen: func(path string) error {
				f, err := os.Create(path)
				if err != nil {
					return err
				}
				defer f.Close()
				return cmd.NewCargoshipCommand().GenFishCompletion(f, true)
			},
		},
		{
			name:     "powershell",
			filename: "cargoship.ps1",
			gen: func(path string) error {
				f, err := os.Create(path)
				if err != nil {
					return err
				}
				defer f.Close()
				return cmd.NewCargoshipCommand().GenPowerShellCompletionWithDesc(f)
			},
		},
	}

	for _, s := range shells {
		targetPath := filepath.Join(targetDir, s.filename)
		if err := s.gen(targetPath); err != nil {
			return fmt.Errorf("generating %s completion: %w", s.name, err)
		}
		fmt.Printf("Generated %s\n", targetPath)
	}

	return nil
}
