#!/bin/bash
# Builds every release file into dist/:
#   arubox-<version>-windows-x64.exe    the program for Windows on x64, which
#   arubox-<version>-windows-arm64.exe  is also its update package, and on Arm
#   manifest.json (+ manifest.json.sig) what the in-app updater trusts
#
#   scripts/package.sh [-k update-signing-key.pem]
#
# The program is the one executable, cross-compiled without cgo, so this
# runs anywhere Go, Task, jq and OpenSSL do. Without -k the manifest is
# unsigned and the in-app updater will not accept the release.
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

task build ARCH=amd64
mv bin/arubox.exe bin/arubox-windows-x64.exe
task build ARCH=arm64
mv bin/arubox.exe bin/arubox-windows-arm64.exe

dist=$project/dist
rm -rf "$dist"
mkdir "$dist"
cp bin/arubox-windows-x64.exe "$dist/arubox-$version-windows-x64.exe"
cp bin/arubox-windows-arm64.exe "$dist/arubox-$version-windows-arm64.exe"

# Prints a platform's entry of the manifest: where its update package is
# and its size and digest.
platform() {
  local file=$dist/$2
  jq -n --arg key "$1" --arg url "https://github.com/shirumesu/arubox/releases/download/v$version/$2" \
    --arg sha256 "$(sha256sum "$file" | cut -d' ' -f1)" --argjson size "$(wc -c <"$file" | tr -d ' ')" \
    '{($key): {urls: [$url], sha256: $sha256, size: $size}}'
}
manifest=$dist/manifest.json
{
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
