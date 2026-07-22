#!/bin/zsh
# Ensure a stable local codesigning identity "Jarvis Local" exists in the
# login keychain. TCC (Full Disk Access / Photos / …) keys off this identity
# across rebuilds — without it, every `go build` looks like a new binary.
set -euo pipefail

identity=${JARVIS_CODESIGN_IDENTITY:-Jarvis Local}
script_dir=${0:A:h}
repo_dir=${script_dir:h}
material_dir=${JARVIS_CODESIGN_DIR:-$repo_dir/var/codesign}
cnf=$script_dir/codesign/openssl-codesign.cnf
keychain=${JARVIS_CODESIGN_KEYCHAIN:-$HOME/Library/Keychains/login.keychain-db}
p12_pass=${JARVIS_CODESIGN_P12_PASS:-jarvis-local-codesign}

if security find-identity -v -p codesigning "$keychain" 2>/dev/null | grep -F "\"$identity\"" >/dev/null; then
  echo "codesign identity already present: $identity"
  security find-identity -v -p codesigning "$keychain" | grep -F "\"$identity\"" || true
  exit 0
fi

if [[ ! -f $cnf ]]; then
  echo "missing openssl config: $cnf" >&2
  exit 1
fi

mkdir -p "$material_dir"
key=$material_dir/jarvis-local.key
crt=$material_dir/jarvis-local.crt
p12=$material_dir/jarvis-local.p12

echo "creating self-signed codesign certificate: $identity"
openssl genrsa -out "$key" 2048
openssl req -new -x509 \
  -key "$key" \
  -out "$crt" \
  -days 3650 \
  -config "$cnf" \
  -extensions v3_codesign
openssl pkcs12 -export \
  -inkey "$key" \
  -in "$crt" \
  -out "$p12" \
  -passout "pass:$p12_pass" \
  -name "$identity" \
  -certpbe PBE-SHA1-3DES \
  -keypbe PBE-SHA1-3DES \
  -macalg sha1 \
  2>/dev/null \
  || openssl pkcs12 -export -legacy \
    -inkey "$key" \
    -in "$crt" \
    -out "$p12" \
    -passout "pass:$p12_pass" \
    -name "$identity"

# Allow codesign to use the key without interactive ACL prompts.
security import "$p12" \
  -k "$keychain" \
  -P "$p12_pass" \
  -A \
  -T /usr/bin/codesign \
  -T /usr/bin/security \
  >/dev/null

# Prefer code-signing trust for this cert (best-effort; some macOS versions differ).
security add-trusted-cert -d -r trustAsRoot -p codeSign -k "$keychain" "$crt" 2>/dev/null \
  || security add-trusted-cert -d -r trustRoot -k "$keychain" "$crt" 2>/dev/null \
  || true

# Unlock private key for codesign in non-interactive shells (best-effort).
security set-key-partition-list \
  -S apple-tool:,apple:,codesign: \
  -s \
  -k "" \
  "$keychain" >/dev/null 2>&1 || true

if ! security find-identity -v -p codesigning "$keychain" 2>/dev/null | grep -F "\"$identity\"" >/dev/null; then
  echo "failed to install codesign identity \"$identity\"" >&2
  echo "open Keychain Access, import $p12, set Trust→Code Signing to Always Trust, then re-run." >&2
  exit 1
fi

echo "codesign identity ready: $identity"
echo "materials kept under $material_dir (gitignored via /var/)"
security find-identity -v -p codesigning "$keychain" | grep -F "\"$identity\"" || true
