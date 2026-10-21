#!/bin/sh
# Builds "D200 Deck.app" around a deckhost binary, signs it ad hoc and zips it
# into $OUT/dist. Runs on a Mac (codesign, ditto), from the repo root.
# Usage: package.sh PKG_VERSION BINARY
set -eu
ver=$1 bin=$2
OUT=${OUT:-build}
stage=$OUT/stage/macos
app="$stage/D200 Deck.app"
rm -rf "$stage"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources" "$OUT/dist"
cp "$bin" "$app/Contents/MacOS/deckhost"
cp contrib/icons/deckhost.icns "$app/Contents/Resources/"
# a bundle version is numbers only: 0.0.0~git... is 0.0.0
sed "s|@VERSION@|${ver%%~*}|g" contrib/macos/Info.plist > "$app/Contents/Info.plist"
plutil -lint "$app/Contents/Info.plist"
codesign --force --sign - "$app"
codesign --verify --strict "$app"
ditto -c -k --keepParent "$app" "$OUT/dist/deckhost-$ver-macos-arm64.zip"
