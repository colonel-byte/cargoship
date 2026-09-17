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

// Package lang holds the cli helping text
package lang

const (
	// CmdDistroCreateShort create short
	CmdDistroCreateShort = "Creates a Cargoship Package from a given directory or the current director"
	// CmdDistroPublishShort publish short
	CmdDistroPublishShort = "Publish the Cargoship Package to an OCI registry"
	// CmdPackagePullShort pull short
	CmdPackagePullShort = "Pulls a Cargoship package from a remote registry and save to the local file system"
	// CmdPackagePullFlagShasum pull shasum flag
	CmdPackagePullFlagShasum = "Shasum of the package to pull"
	// CmdDistroApplyShort apply short
	CmdDistroApplyShort = "Apply a config file to bootstrap and upgrade a cluster"
	// CmdDistroPrepareShort prepare short
	CmdDistroPrepareShort = "Prepares the nodes, including restarting the node if new kernel modules are enabled"
	// CmdDistroResetShort reset short
	CmdDistroResetShort = "Reset a cluster, stopping, uninstalling, and removing all data for a engine"
	// CmdDistroKubeConfigShort kube-config short
	CmdDistroKubeConfigShort = "Get the admin kube-config for a control-plane node"
	// CmdDistroEngineConfigSyncShort engine-config-sync short
	CmdDistroEngineConfigSyncShort = "Sync engine config (registries, audit, and pod security) to a cluster, draining and restarting the engine service on any node whose config has drifted"
	// CmdInstallFapolicydUpdate install flag fapolicyd
	CmdInstallFapolicydUpdate = "Whether to update all the host nodes fapolicyd configuration."
	// CmdInstallFirewallUpdate install flag firewall
	CmdInstallFirewallUpdate = "Whether to update all the host nodes firewall configuration."
	// CmdInstallFlagConcurrency install flag concurrency
	CmdInstallFlagConcurrency = "Maximum number of hosts to configure in parallel, set to 0 for unlimited."
	// CmdInstallFlagConfig install flag config
	CmdInstallFlagConfig = "Config file used to bootstrap a cluster."
	// CmdInstallFlagResetDistro install flag config
	CmdInstallFlagResetDistro = "What type of distro that will be reset. Valid options are: 'rke2', 'k3s'."
	// CmdInstallFlagKubeConfigDistro kube-config flag config
	CmdInstallFlagKubeConfigDistro = "What type of distro we will get the admin config from. Valid options are: 'rke2', 'k3s'."
	// CmdInstallFlagConfirm install flag confirm
	CmdInstallFlagConfirm = "Confirm whether if to proceed with the install"
	// CmdInstallFlagDryRun install flag dry run
	CmdInstallFlagDryRun = "Report what would be done without changing any host. Connects to every host and runs the preflight checks for real, then lists the phases it did not run. Does not need --confirm."
	// CmdInstallFlagTimeout install flag timeout
	CmdInstallFlagTimeout = "Set the timeout for how long functions will last."
	// CmdInstallFlagValues install flag values
	CmdInstallFlagValues = "Path to a YAML values file overriding the values the package ships with. May be given more than once, with a later file winning over an earlier one, and all of them winning over the values in the cluster config file."
	// CmdInstallFlagVaultPasswordFile install flag vault password file
	CmdInstallFlagVaultPasswordFile = "Path to a file containing the Ansible Vault password used to decrypt vault-encrypted registry credentials. Falls back to the CARGOSHIP_VAULT_PASSWORD, then ANSIBLE_VAULT_PASSWORD, environment variable."
	// CmdInstallFlagWorkerConcurrency install flag worker concurrency
	CmdInstallFlagWorkerConcurrency = "Maximum number of workers that will be installed or updated in parallel, as a fixed count or a percentage (e.g. \"25%\"), set to 0 for unlimited."
	// CmdInstallHostUpdate install flag host
	CmdInstallHostUpdate = "Whether to update all the host nodes /etc/hosts file."
	// CmdInstallAllowUnmanagedNodes install flag allow unmanaged nodes
	CmdInstallAllowUnmanagedNodes = "Continue when the cluster holds a node that no host in the config accounts for. An apply never removes a node, so by default one left behind by a host deleted from the config stops the run. Set this when the extra nodes were joined deliberately and cargoship should leave them alone."
	// CmdInstallLabelNodes install flag label nodes
	CmdInstallLabelNodes = "Whether to check and add the node-role.kubernetes.io/<profile> label on cluster nodes. Requires --update-kubeconfig."
	// CmdInstallKubeConfigPath install flag kubeconfig path
	CmdInstallKubeConfigPath = "Path of the kubeconfig file to merge the admin creds for this cluster into. The file is created when it does not exist, and an existing one keeps every other cluster it holds. Defaults to the standard location: KUBECONFIG when set, otherwise ~/.kube/config."
	// CmdInstallUpdateKubeConfig install flag update kubeconfig
	CmdInstallUpdateKubeConfig = "Whether to write the admin creds for this cluster to a kubeconfig file at all."
	// CmdPackageCreateFlagOutput create flag output
	CmdPackageCreateFlagOutput = "Specify the output (either a directory or an oci:// URL) for the created Zarf distro package"
	// CmdPackageFlagConcurrency deploy flag concurrency
	CmdPackageFlagConcurrency = "Number of concurrent layer operations when pulling or pushing images or packages to/from OCI registries."
	// CmdPackageFlagRetries publish flag retry
	CmdPackageFlagRetries = "Number of retries to perform for Cargoships operations like package publishes"
	// CmdVersionLong version long
	CmdVersionLong = "Displays the version of the release that the current binary was built from."
	// CmdVersionShort version short
	CmdVersionShort = "Shows the version of the running binary"
	// CmdVersionOutputFromat version flag output format
	CmdVersionOutputFromat = "output format (yaml|json)"
	// CmdSha256SumShort sha256sum short
	CmdSha256SumShort = "Generates a SHA256SUM for the given file"
	// CmdSha256SumFlagExtractPath flag description
	CmdSha256SumFlagExtractPath = `The path inside of an archive to use to calculate the sha256sum (i.e. for use with "files.extractPath")`
	// CmdVaultShort vault short
	CmdVaultShort = "Encrypts and decrypts cluster configuration values with Ansible Vault or age"
	// CmdVaultLong vault long
	CmdVaultLong = "Groups the commands that read and write encrypted values for a cluster configuration, in either of the two formats cargoship supports: Ansible Vault, keyed by a shared password given via --vault-password-file or the CARGOSHIP_VAULT_PASSWORD environment variable, and age, keyed by public keys given via --age-recipient and read back with --age-identity-file. Cargoship decrypts a registry's user/pass/token and tls.ca fields at apply time. Which format a value is in is read from the value itself, so one configuration can hold both, and moving between them is 'cargoship vault rekey'. The group is still called vault because renaming it would break every script that calls it."
	// CmdVaultEncryptDeprecated deprecation notice for the top-level vault-encrypt spelling
	CmdVaultEncryptDeprecated = `use "cargoship vault encrypt" instead.`
	// CmdVaultEncryptShort vault encrypt short
	CmdVaultEncryptShort = "Encrypts a value with Ansible Vault or age, for use in a registry's user/pass/token fields"
	// CmdVaultEncryptLong vault encrypt long
	CmdVaultEncryptLong = "Encrypts VALUE, producing a string that cargoship decrypts automatically at apply time when placed in a registry's user/pass/token field. With --age-recipient or --age-recipients-file the output is armored age ciphertext beginning with -----BEGIN AGE ENCRYPTED FILE-----; otherwise it is Ansible Vault ciphertext beginning with $ANSIBLE_VAULT. If VALUE is omitted, it is read from stdin, or prompted for with hidden input when stdin is a terminal."
	// CmdVaultEncryptFlagPasswordFile flag description
	CmdVaultEncryptFlagPasswordFile = "Path to a file containing the Ansible Vault password. Falls back to the CARGOSHIP_VAULT_PASSWORD, then ANSIBLE_VAULT_PASSWORD, environment variable."
	// CmdFlagAgeIdentityFile flag description
	CmdFlagAgeIdentityFile = "Path to an age identity file holding the private keys that decrypt registry credentials, or to an SSH private key such as ~/.ssh/id_ed25519. Repeatable; also settable as age.identity_files in the cargoship config file, or as a single path in CARGOSHIP_AGE_IDENTITY_FILE."
	// CmdFlagAgeRecipient flag description
	CmdFlagAgeRecipient = "A public key to encrypt registry credentials to: either an age recipient such as age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p, or an SSH public key such as 'ssh-ed25519 AAAAC3Nza...'. Repeatable; also settable as age.recipients in the cargoship config file, or space-separated in CARGOSHIP_AGE_RECIPIENTS. Giving any recipient makes cargoship write age ciphertext instead of Ansible Vault."
	// CmdFlagAgeRecipientsFile flag description
	CmdFlagAgeRecipientsFile = "Path to a file holding public keys, one per line, which may mix age recipients and SSH public keys; an authorized_keys file works as it is. Repeatable; also settable as age.recipients_files in the cargoship config file."
	// CmdVaultEncryptPathShort vault encrypt-path short
	CmdVaultEncryptPathShort = "Encrypts the values a config file already holds at one or more YAML paths, in place"
	// CmdVaultEncryptPathLong vault encrypt-path long
	CmdVaultEncryptPathLong = "Encrypts the values FILE holds at each YAML_PATH and writes them back to FILE as block scalars, leaving comments, key order, and the rest of the document untouched. Each YAML_PATH names a value inside FILE rather than a file on disk: it is a YAML path such as '.spec.config.registries[0].auth.pass', and the leading '$' go-yaml uses is optional. Quote it, since it usually contains characters a shell would otherwise expand. Give as many as you like -- FILE is written once, after every one of them has encrypted, so a path that is missing or encrypted already leaves FILE as it was rather than partly rewritten. Cargoship decrypts a registry's user/pass/token and tls.ca fields at apply time, and warns about a path anywhere else, because nothing unwraps a value encrypted elsewhere."
	// CmdVaultEncryptPathFlagDryRun flag description
	CmdVaultEncryptPathFlagDryRun = "Print the resulting document to stdout instead of writing it back to FILE."
	// CmdVaultEncryptPathFlagForce flag description
	CmdVaultEncryptPathFlagForce = "Encrypt the value even though it is encrypted already, wrapping it a second time."
	// CmdVaultEncryptFileShort vault encrypt-file short
	CmdVaultEncryptFileShort = "Encrypts every registry credential in a config file, in place"
	// CmdVaultEncryptFileLong vault encrypt-file long
	CmdVaultEncryptFileLong = "Encrypts every registry credential FILE holds that cargoship decrypts at apply time -- each registry's auth.user, auth.pass, auth.token and tls.ca -- and writes them back to FILE as block scalars, leaving comments, key order, and the rest of the document untouched. A field that is absent, empty, or encrypted already is skipped, so running this over a partly encrypted configuration finishes the job and running it twice changes nothing the second time. That skip applies to a value in the other format too: this command never moves a credential between Ansible Vault and age, which is what 'cargoship vault rekey' is for. Every credential skipped for a reason worth knowing about is reported, so a run that changes nothing says which values it left alone and why."
	// CmdVaultEncryptFileFlagDryRun flag description
	CmdVaultEncryptFileFlagDryRun = "Print the resulting document to stdout instead of writing it back to FILE."
	// CmdVaultEncryptFileFlagForce flag description
	CmdVaultEncryptFileFlagForce = "Encrypt values that are encrypted already, wrapping them a second time."
	// CmdVaultRekeyFlagNewPasswordFile flag description
	CmdVaultRekeyFlagNewPasswordFile = "Path to a file containing the Ansible Vault password to move to. Omit it to re-salt every value under the key the file already uses, or pass --age-recipient instead to move the file onto age. Deliberately without an environment fallback: the environment holds the password the file is vaulted under now, so an omitted flag would otherwise look like a rotation that never happened."
	// CmdVaultRekeyFlagDryRun flag description
	CmdVaultRekeyFlagDryRun = "Print the resulting document to stdout instead of writing it back to FILE."
	// CmdVaultKeygenShort vault keygen short
	CmdVaultKeygenShort = "Generates an age key pair, or prints the public key of one you already hold"
	// CmdVaultKeygenLong vault keygen long
	CmdVaultKeygenLong = "Generates an age key pair in the format age-keygen writes, so that the age distribution is not a prerequisite for encrypting a configuration with age, and the file stays readable by 'age --decrypt' and anything else that speaks the format. The identity -- the private key, AGE-SECRET-KEY-1... -- goes to --output, or to stdout when that is omitted; the recipient -- the public key, age1... -- goes to stderr, so that the file holds the private key alone and the public key can be copied straight into a recipients file. With --public-key it generates nothing and instead prints the public keys held in IDENTITY_FILE, or in stdin when no file is given, which is how a recipient is recovered from a private key you still have. A key pair is not registered anywhere: pass the public key to 'cargoship vault encrypt' as --age-recipient, and the file back as --age-identity-file to read those values again."
	// CmdVaultKeygenFlagOutput flag description
	CmdVaultKeygenFlagOutput = "Path to write the generated identity to, created with mode 0600. An existing file is never overwritten, because anything encrypted to the key it holds would become unreadable. Omit it to write to stdout instead."
	// CmdVaultKeygenFlagPublicKey flag description
	CmdVaultKeygenFlagPublicKey = "Print the public keys held in IDENTITY_FILE instead of generating a key pair. Reads stdin when no file is given. This reads age identity files; the public key of an SSH key is in the \".pub\" file beside it."
	// CmdVaultDecryptShort vault decrypt short
	CmdVaultDecryptShort = "Decrypts an Ansible Vault or age value, printing the plaintext"
	// CmdVaultDecryptLong vault decrypt long
	CmdVaultDecryptLong = "Decrypts VALUE, a string produced by 'cargoship vault encrypt' in either format, and prints the plaintext to stdout. The format is read from the value itself: a $ANSIBLE_VAULT prefix needs the vault password, an -----BEGIN AGE ENCRYPTED FILE----- prefix needs --age-identity-file. If VALUE is omitted, it is read from stdin, which is the easier way to hand over an age value, since one begins with dashes and is read as a flag when given as an argument; after the flags, '--' works too. The plaintext is written as it is, with a trailing newline added only when it does not already end in one, so that a multi-line value can be redirected straight into a file."
	// CmdVaultDecryptPathShort vault decrypt-path short
	CmdVaultDecryptPathShort = "Decrypts the values a config file holds at one or more YAML paths, in place"
	// CmdVaultDecryptPathLong vault decrypt-path long
	CmdVaultDecryptPathLong = "Decrypts the encrypted values FILE holds at each YAML_PATH and writes the plaintext back to FILE, leaving comments, key order, and the rest of the document untouched. Each YAML_PATH names a value inside FILE rather than a file on disk: it is a YAML path such as '.spec.config.registries[0].auth.pass', and the leading '$' go-yaml uses is optional. Quote it, since it usually contains characters a shell would otherwise expand. Give as many as you like -- FILE is written once, after every one of them has decrypted, so a path that is missing or plaintext already leaves FILE as it was. This is the inverse of 'cargoship vault encrypt-path', and leaves the values readable to anyone who can read the file."
	// CmdVaultDecryptPathFlagDryRun flag description
	CmdVaultDecryptPathFlagDryRun = "Print the resulting document to stdout instead of writing it back to FILE."
	// CmdVaultDecryptFileShort vault decrypt-file short
	CmdVaultDecryptFileShort = "Decrypts every registry credential in a config file, in place"
	// CmdVaultDecryptFileLong vault decrypt-file long
	CmdVaultDecryptFileLong = "Decrypts every encrypted registry credential FILE holds -- each registry's auth.user, auth.pass, auth.token and tls.ca -- and writes the plaintext back to FILE, leaving comments, key order, and the rest of the document untouched. A field that is not encrypted is skipped. This is the inverse of 'cargoship vault encrypt-file', and leaves the credentials readable to anyone who can read the file."
	// CmdVaultRekeyShort vault rekey short
	CmdVaultRekeyShort = "Re-wraps every encrypted registry credential in a config file, optionally under a new key"
	// CmdVaultRekeyLong vault rekey long
	CmdVaultRekeyLong = "Re-wraps every encrypted registry credential FILE holds -- each registry's auth.user, auth.pass, auth.token and tls.ca -- under the key named by --new-vault-password-file or by --age-recipient, leaving comments, key order, and the rest of the document untouched. Naming age recipients while reading with the vault password is how a configuration moves from Ansible Vault to age; naming neither re-wraps the values under the key they already carry, which gives every one of them fresh ciphertext without changing the key. The plaintext is never written to FILE: each value is decrypted and encrypted again in memory, which is what makes this safer than a decrypt-file followed by an encrypt-file, where the file holds the credentials in the clear in between. Every encrypted value has to be readable with the keys given, and the command stops without touching FILE if one is not, because a configuration encrypted under two keys is one no single key can read back."
	// CmdVaultDecryptFileFlagDryRun flag description
	CmdVaultDecryptFileFlagDryRun = "Print the resulting document to stdout instead of writing it back to FILE."
	// CmdViperErrLoadingConfigFile error text
	CmdViperErrLoadingConfigFile = "failed to load config file"
	// RootCmdFlagLogFormat log format
	RootCmdFlagLogFormat = "Select a logging format. Defaults to 'console'. Valid options are: 'console', 'json', 'dev'."
	// RootCmdFlagLogLevel log level
	RootCmdFlagLogLevel = "Log level when running cargoship. Valid options are: warn, info, debug, trace"
	// RootCmdFlagNoColor no color
	RootCmdFlagNoColor = "Disable terminal color codes in logging and stdout prints."
	// RootCmdFlagLogFile log file
	RootCmdFlagLogFile = "Always write a full-verbosity debug log to a file, regardless of --log-level."
	// RootCmdShort root short
	RootCmdShort = "CLI for cargoship installs"
	// RootCmdUse root use
	RootCmdUse = "cargoship COMMAND"
	// RootGroupInstallID subcommand for install id
	RootGroupInstallID = "install"
	// RootGroupInstallTitle subcommand for install title
	RootGroupInstallTitle = "Install Commands:"
	// RootGroupPackageID subcommand for package id
	RootGroupPackageID = "package"
	// RootGroupPackageTitle subcommand for package id
	RootGroupPackageTitle = "Package Commands:"
	// CmdPackageFlagVerify flag
	CmdPackageFlagVerify = "Verify the Cargoship package signature"
	// CmdPackageCreateFlagReproducible create flag reproducible
	CmdPackageCreateFlagReproducible = "Pin the recorded package build time to a fixed value instead of the current time, so identical inputs produce a byte-identical package."
	// CmdDistroSignShort sign short
	CmdDistroSignShort = "Signs an existing Cargoship distro package"
	// CmdDistroSignLong sign long
	CmdDistroSignLong = "Signs an existing Cargoship distro package with a private key. The package can be a local tarball or pulled from an OCI registry. The signature is created by signing the distro.yaml file and does not modify the package checksums."
)
