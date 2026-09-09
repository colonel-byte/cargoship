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

package clustercfg

import (
	"fmt"
	"os"
	"strings"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	vault "github.com/sosedoff/ansible-vault-go"
)

// VaultPasswordEnvVar is the environment variable checked for the Ansible Vault
// password when no --vault-password-file flag is given.
const VaultPasswordEnvVar = "CARGOSHIP_VAULT_PASSWORD"

// AnsibleVaultPasswordEnvVar is a secondary environment variable checked for the
// Ansible Vault password, used by ansible-vault itself. VaultPasswordEnvVar takes
// precedence when both are set.
const AnsibleVaultPasswordEnvVar = "ANSIBLE_VAULT_PASSWORD"

// ResolveVaultPassword returns the Ansible Vault password to use for decrypting
// registry credentials. If passwordFile is set, its contents are read and used.
// Otherwise it falls back to the CARGOSHIP_VAULT_PASSWORD environment variable,
// then to ANSIBLE_VAULT_PASSWORD. An empty return value with a nil error means
// no password was configured.
func ResolveVaultPassword(passwordFile string) (string, error) {
	if passwordFile != "" {
		b, err := os.ReadFile(passwordFile)
		if err != nil {
			return "", fmt.Errorf("reading vault password file: %w", err)
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	if password := os.Getenv(VaultPasswordEnvVar); password != "" {
		return password, nil
	}
	return os.Getenv(AnsibleVaultPasswordEnvVar), nil
}

// DecryptRegistryAuth decrypts any Ansible Vault-encrypted Username, Password,
// or Token fields on dis.Spec.Config.Registries in place, along with an inline
// TLS CA certificate given the same way. Fields that don't carry the
// $ANSIBLE_VAULT header are left untouched.
//
// A CA certificate is public and does not need encrypting, but accepting one
// encrypted means a document can be vaulted as a whole without cargoship
// rejecting the parts that did not have to be. The decrypted TLS settings are
// validated here, since load time saw only ciphertext.
func DecryptRegistryAuth(dis *cluster.ZarfCluster, password string) error {
	registries := dis.Spec.Config.Registries
	for i := range registries {
		auth := &registries[i].Authentication
		// Named so that an error can say which value could not be read rather than only which
		// registry it belonged to. A document usually vaults more than one of them.
		fields := []struct {
			name  string
			value *string
		}{
			{"auth.user", &auth.Username},
			{"auth.pass", &auth.Password},
			{"auth.token", &auth.Token},
		}
		if registries[i].TLS != nil {
			fields = append(fields, struct {
				name  string
				value *string
			}{"tls.ca", &registries[i].TLS.CA})
		}
		decrypted := false
		for _, field := range fields {
			if !cluster.IsVaultEncrypted(*field.value) {
				continue
			}
			if password == "" {
				return fmt.Errorf("registry %q: %s is Ansible Vault-encrypted but no vault password was provided; pass --vault-password-file or set %s", registries[i].Name, field.name, VaultPasswordEnvVar)
			}
			plain, err := vault.Decrypt(*field.value, password)
			if err != nil {
				return fmt.Errorf("registry %q: decrypting %s: %w", registries[i].Name, field.name, err)
			}
			*field.value = plain
			decrypted = true
		}
		if decrypted {
			if err := registries[i].TLS.Validate(registries[i].Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// EncryptValue encrypts value with the given Ansible Vault password, producing
// a string suitable for use as a registry auth field (see DecryptRegistryAuth).
func EncryptValue(value, password string) (string, error) {
	encrypted, err := vault.Encrypt(value, password)
	if err != nil {
		return "", fmt.Errorf("encrypting value: %w", err)
	}
	return encrypted, nil
}
