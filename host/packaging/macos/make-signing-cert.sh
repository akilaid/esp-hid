#!/usr/bin/env bash
# Creates the self-signed code-signing certificate that keeps macOS's
# Accessibility and Input Monitoring grants across updates, installs it in
# the login keychain so local builds use it, and exports it for the release
# workflow.
#
# Usage: ./make-signing-cert.sh [output.p12]
#
# Run it once, on the machine you build on. Afterwards:
#   - build-macos.sh signs with it automatically (it finds the certificate by
#     name), and
#   - the printed `gh secret set` commands give CI the same certificate, so
#     releases and local builds share one signature.
#
# The certificate is valid for ten years. macOS ties the grants to the
# certificate, not to its expiry, but codesign will not sign with an expired
# one, so at that point run this again and re-grant once.
#
# Nothing here needs an Apple Developer account. The trade is that the app
# is still not notarized: the first install of a browser-downloaded copy
# still needs right-click -> Open. Updates installed by the app itself never
# pass through Gatekeeper.

set -euo pipefail

NAME="ESP HID Bridge"
OUT="${1:-$HOME/Desktop/esp-hid-signing.p12}"
LOGIN_KEYCHAIN="$HOME/Library/Keychains/login.keychain-db"

if security find-identity -p codesigning 2>/dev/null | grep -q "\"$NAME\""; then
  echo "A \"$NAME\" code-signing identity already exists in the keychain."
  echo "Delete it in Keychain Access first if you mean to replace it — a new"
  echo "certificate means re-granting the permissions once."
  exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

cat >"$work/openssl.cnf" <<CNF
[req]
distinguished_name = dn
x509_extensions = codesign
prompt = no
[dn]
CN = $NAME
[codesign]
keyUsage = critical, digitalSignature
extendedKeyUsage = critical, codeSigning
basicConstraints = critical, CA:false
subjectKeyIdentifier = hash
CNF

echo "Creating the certificate"
openssl req -x509 -newkey rsa:2048 -sha256 -days 3650 -nodes \
  -keyout "$work/key.pem" -out "$work/cert.pem" -config "$work/openssl.cnf" 2>/dev/null
openssl x509 -in "$work/cert.pem" -outform DER -out "$work/cert.cer"

# The .p12 password protects the private key at rest and in the CI secret.
PASSWORD="$(openssl rand -hex 16)"
openssl pkcs12 -export -inkey "$work/key.pem" -in "$work/cert.pem" \
  -name "$NAME" -out "$OUT" -passout "pass:$PASSWORD"

echo "Installing it in the login keychain (macOS may ask for your password)"
security import "$OUT" -k "$LOGIN_KEYCHAIN" -P "$PASSWORD" -T /usr/bin/codesign >/dev/null
# A self-signed certificate is its own root; codesign only accepts it once it
# is trusted for code signing.
security add-trusted-cert -r trustRoot -p codeSign -k "$LOGIN_KEYCHAIN" "$work/cert.cer"

if ! security find-identity -v -p codesigning | grep -q "\"$NAME\""; then
  echo "The identity was imported but is not listed as valid for code signing." >&2
  echo "Open Keychain Access, find \"$NAME\", Get Info > Trust, and set Code Signing to Always Trust." >&2
  exit 1
fi

cat <<MSG

Done. Local builds now sign as "$NAME".

Give the release workflow the same certificate (run from the repo):

  gh secret set MACOS_SIGNING_CERT_P12 --repo akilaid/esp-hid < <(base64 -i "$OUT")
  gh secret set MACOS_SIGNING_CERT_PASSWORD --repo akilaid/esp-hid --body "$PASSWORD"

Then keep $OUT and the password somewhere safe (a password manager);
they are the only copy of the identity, and losing them means a new
certificate and one more round of permission grants for every user.
MSG
