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

package main

// A simple file uploader for testing
import (
	"flag"
	"fmt"
	goos "os"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
	zconfig "github.com/zarf-dev/zarf/src/config"

	// anonymous import is needed to load the os configurers
	_ "github.com/colonel-byte/zarf-distro/src/types/os"
	// anonymous import is needed to load the os configurers
	_ "github.com/colonel-byte/zarf-distro/src/types/os/linux"
	// anonymous import is needed to load the os configurers
	_ "github.com/colonel-byte/zarf-distro/src/types/os/linux/enterpriselinux"
)

type configurer interface {
	Pwd(host os.Host) string
	CheckPrivilege(host os.Host) error
}

// Host is a host that utilizes rig for connections
type Host struct {
	rig.Connection

	Configurer configurer
}

// LoadOS is a function that assigns a OS support package to the host and
// typecasts it to a suitable interface
func (h *Host) LoadOS() error {
	bf, err := registry.GetOSModuleBuilder(*h.OSVersion)
	if err != nil {
		return err
	}

	c, ok := bf().(configurer)
	if !ok {
		return fmt.Errorf("OS %s does not support configurer interface", *h.OSVersion)
	}
	h.Configurer = c

	return nil
}

func main() {
	dh := flag.String("host", "127.0.0.1", "target host")
	dp := flag.Int("port", 3000, "target host port")
	sf := flag.String("src", "tmpfile", "source file")
	df := flag.String("dst", "/tmp/tempfile", "destination file")
	sudo := flag.Bool("sudo", false, "use sudo when uploading")
	usr := flag.String("user", "root", "user name")
	pwd := flag.String("pass", "", "password")
	key := flag.String("key", "~/.ssh/id_ed25519", "ssh key")
	proto := flag.String("proto", "ssh", "ssh/winrm")
	config := flag.String("config", "~/.ssh/config", "user ssh config")
	https := flag.Bool("https", false, "use https")

	flag.Parse()

	keys, err := zconfig.GetAbsHomePath(*key)
	if err != nil {
		panic(err)
	}

	configs, err := zconfig.GetAbsHomePath(*config)
	if err != nil {
		panic(err)
	}

	if *dh == "" {
		println("see -help")
		goos.Exit(1)
	}

	var h *Host

	if *proto == "ssh" {
		h = &Host{
			Connection: rig.Connection{
				OpenSSH: &rig.OpenSSH{
					Address:    *dh,
					Port:       dp,
					User:       usr,
					KeyPath:    &keys,
					ConfigPath: &configs,
				},
			},
		}
	} else {
		h = &Host{
			Connection: rig.Connection{
				WinRM: &rig.WinRM{
					Address:  *dh,
					Port:     *dp,
					User:     *usr,
					UseHTTPS: *https,
					Insecure: true,
					Password: *pwd,
				},
			},
		}
	}

	if err := h.Connect(); err != nil {
		fmt.Println(*dh, *dp)
		panic(err)
	}

	if err := h.LoadOS(); err != nil {
		panic(err)
	}

	var opts []exec.Option
	if *sudo {
		opts = append(opts, exec.Sudo(h))
	}
	if err := h.Upload(*sf, *df, 0o600, opts...); err != nil {
		panic(err)
	}
	fmt.Println("Done, file now at", *df)
}
