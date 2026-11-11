#!/bin/bash
# Builds every release file into dist/, on a Mac:
#   arubox-<version>-macos-arm64.dmg    disk image to drag into Applications
#   arubox-<version>-macos-arm64.zip    the app bundle; the macOS update package
#   arubox-<version>-windows-x64.exe    the program for Windows on x64, which
#   arubox-<version>-windows-arm64.exe  is also its update package, and on Arm
#   manifest.json (+ manifest.json.sig) what the in-app updater trusts
#
#   ARUBOX_SIGN_IDENTITY=<code-signing identity> scripts/package.sh [-k update-signing-key.pem]
#
# The bundle must be signed with the arubox certificate, never ad hoc:
# macOS ties the Accessibility and Screen Recording permissions to the
# signature's designated requirement, which for an ad hoc signature is the
# hash of this very build, so every update would lose them. With the
# certificate it is the bundle ID and the certificate, the same for every
# version. Windows needs no installer and no signature here: the program
# is the one executable, cross-compiled without cgo.
#
# Without -k the manifest is unsigned and the in-app updater will not
# accept the release.
set -euo pipefail

signing_key=
while getopts 'k:' option; do
  case $option in
    k) signing_key=$OPTARG ;;
    *) exit 2 ;;
  esac
done

fail() {
  echo "$1" >&2
  exit 1
}

project=$(cd "$(dirname "$0")/.." && pwd)
cd "$project"
[[ -n ${ARUBOX_SIGN_IDENTITY:-} && $ARUBOX_SIGN_IDENTITY != - ]] ||
  fail 'Set ARUBOX_SIGN_IDENTITY to the arubox code-signing identity; a release is never signed ad hoc.'
if [[ -n $signing_key ]]; then
  signing_key=$(cd "$(dirname "$signing_key")" && pwd)/$(basename "$signing_key")
fi
version=$(task --silent version)

# The release notes are CHANGELOG.md without its heading, which names the
# version.
heading=$(head -n 1 CHANGELOG.md)
[[ $heading == "# arubox $version" ]] || fail "CHANGELOG.md is for ${heading#\# }, not arubox $version."
notes=$(tail -n +2 CHANGELOG.md)
[[ -n ${notes//[[:space:]]/} ]] || fail 'CHANGELOG.md has no notes under its heading.'

task build
task build:windows ARCH=amd64
mv bin/arubox.exe bin/arubox-windows-x64.exe
task build:windows ARCH=arm64
mv bin/arubox.exe bin/arubox-windows-arm64.exe

app=bin/arubox.app
codesign --verify --strict "$app"
requirement=$(codesign -d -r- "$app" 2>&1)
[[ $requirement == *'certificate leaf'* && $requirement != *cdhash* ]] ||
  fail "The bundle's designated requirement must name the certificate: $requirement"

dist=$project/dist
rm -rf "$dist"
mkdir "$dist"
staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT

mac_zip=arubox-$version-macos-arm64.zip
# Without extended attributes, such as the provenance macOS gives every
# file a process writes, which would travel as ._ files.
ditto -c -k --norsrc --keepParent "$app" "$dist/$mac_zip"
image=$staging/image
mkdir "$image"
ditto "$app" "$image/arubox.app"
ln -s /Applications "$image/Applications"
hdiutil create -quiet -volname arubox -srcfolder "$image" -format ULFO \
  "$dist/arubox-$version-macos-arm64.dmg"
cp bin/arubox-windows-x64.exe "$dist/arubox-$version-windows-x64.exe"
cp bin/arubox-windows-arm64.exe "$dist/arubox-$version-windows-arm64.exe"

# Prints a platform's entry of the manifest: where its update package is
# and its size and digest.
platform() {
  local file=$dist/$2
  jq -n --arg key "$1" --arg url "https://github.com/shirumesu/arubox/releases/download/v$version/$2" \
    --arg sha256 "$(shasum -a 256 "$file" | cut -d' ' -f1)" --argjson size "$(stat -f %z "$file")" \
    '{($key): {urls: [$url], sha256: $sha256, size: $size}}'
}
manifest=$dist/manifest.json
{
  platform macos-arm64 "$mac_zip"
  platform windows-x64 "arubox-$version-windows-x64.exe"
  platform windows-arm64 "arubox-$version-windows-arm64.exe"
} | jq -sc --arg version "$version" --arg notes "$notes" \
  '{version: $version, notes: ($notes | gsub("^\\s+|\\s+$"; "")), platforms: add}' >"$manifest"
if [[ -n $signing_key ]]; then
  openssl dgst -sha256 -sign "$signing_key" -out "$manifest.sig" "$manifest"
else
  echo 'No signing key: the in-app updater will not accept this release.' >&2
fi
ls -l "$dist"
