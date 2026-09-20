#!/usr/bin/env bash
# Copyright 2026 colonel-byte
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

#
# Generate the two key pairs that sign the Linux packages.
#
# Two, not one, because nfpm signs rpm and deb with OpenPGP but signs apk with a bare RSA key.
# Handing the OpenPGP key to the apk signer fails the release with `signing error: no PEM block
# found`. See the note on apk.signature in .goreleaser.yaml.
#
# Usage:
#   hack/gen-signing-keys.sh <output-dir>
#
# Set GPG_PASSPHRASE to protect the OpenPGP key; it is generated unprotected otherwise. The RSA
# key is always unprotected, because nfpm cannot read an encrypted one: it only understands the
# legacy DEK-Info PEM encryption, and OpenSSL 3 emits PKCS#8, which fails with
# `key type "ENCRYPTED PRIVATE KEY" is not supported`. Protect it as a CI secret instead.
#
# Nothing here touches your own keyring: the OpenPGP key is built in a throwaway GNUPGHOME and
# exists only as the exported files.
#
# The private keys become repository secrets. The public keys are published under crypto/ so that
# consumers can verify: see docs/dev/goreleaser.md for where each one goes.
#

set -euo pipefail

output=${1:-}

if [[ -z ${output} ]]; then
	echo "usage: $0 <output-dir>" >&2
	exit 2
fi
for tool in gpg openssl; do
	if ! command -v "${tool}" >/dev/null; then
		echo "$0: ${tool} is not on PATH" >&2
		exit 1
	fi
done

# The apk signature records the key name, and nfpm defaults it to the maintainer address. Reading
# it back out of .goreleaser.yaml rather than hardcoding it keeps the public key's filename in
# step with the name apk will look for under /etc/apk/keys.
maintainer=$(awk '/^[[:space:]]*maintainer:/ {f = 1; next} f && match($0, /<[^>]+>/) {print substr($0, RSTART + 1, RLENGTH - 2); exit}' .goreleaser.yaml)
if [[ -z ${maintainer} ]]; then
	echo "$0: no maintainer address in .goreleaser.yaml" >&2
	exit 1
fi

gpg_secret="${output}/cargoship-signing.asc"
gpg_public="${output}/${maintainer}.gpg"
apk_secret="${output}/cargoship-apk.rsa"
apk_public="${output}/${maintainer}.rsa.pub"

mkdir -p "${output}"
# Refuse rather than clobber. Overwriting a signing key that is already in use invalidates every
# package signed with it, and the private half is not recoverable from anywhere else.
for f in "${gpg_secret}" "${gpg_public}" "${apk_secret}" "${apk_public}"; do
	if [[ -e ${f} ]]; then
		echo "$0: ${f} already exists, refusing to overwrite it" >&2
		exit 1
	fi
done

umask 077

gnupghome=$(mktemp -d)
trap 'rm -rf "${gnupghome}"' EXIT
chmod 700 "${gnupghome}"

if [[ -n ${GPG_PASSPHRASE:-} ]]; then
	protection="Passphrase: ${GPG_PASSPHRASE}"
else
	protection="%no-protection"
fi

echo "$0: generating the OpenPGP key for rpm and deb"
GNUPGHOME=${gnupghome} gpg --batch --quiet --gen-key <<EOF
Key-Type: RSA
Key-Length: 4096
Key-Usage: sign
Name-Real: Cargoship Release Signing
Name-Email: ${maintainer}
Expire-Date: 2y
${protection}
%commit
EOF

fingerprint=$(GNUPGHOME=${gnupghome} gpg --list-secret-keys --with-colons | awk -F: '/^fpr/ {print $10; exit}')
if [[ -z ${fingerprint} ]]; then
	echo "$0: gpg produced no key" >&2
	exit 1
fi

export_args=(--batch --quiet --armor)
if [[ -n ${GPG_PASSPHRASE:-} ]]; then
	export_args+=(--pinentry-mode loopback --passphrase "${GPG_PASSPHRASE}")
fi
GNUPGHOME=${gnupghome} gpg "${export_args[@]}" --export-secret-keys "${fingerprint}" >"${gpg_secret}"
GNUPGHOME=${gnupghome} gpg "${export_args[@]}" --export "${fingerprint}" >"${gpg_public}"

echo "$0: generating the RSA key for apk"
openssl genrsa -out "${apk_secret}" 4096 2>/dev/null
openssl rsa -in "${apk_secret}" -pubout -out "${apk_public}" 2>/dev/null

chmod 600 "${gpg_secret}" "${apk_secret}"
chmod 644 "${gpg_public}" "${apk_public}"

cat <<EOF

OpenPGP key ${fingerprint}

  ${gpg_secret}
      secret -> repository secret GPG_PRIVATE_KEY
  ${gpg_public}
      publish as crypto/${maintainer}.gpg
$(if [[ -n ${GPG_PASSPHRASE:-} ]]; then echo "  passphrase -> repository secret GPG_PASSPHRASE"; fi)

RSA key for apk

  ${apk_secret}
      secret -> repository secret APK_RSA_PRIVATE_KEY
  ${apk_public}
      publish as crypto/${maintainer}.rsa.pub; the name is not cosmetic --
      consumers install it as-is at /etc/apk/keys/${maintainer}.rsa.pub

The two secret files are not protected by anything but their mode. Load them into the repository
secrets and delete them.
EOF
