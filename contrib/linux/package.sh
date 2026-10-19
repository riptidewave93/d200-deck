#!/bin/sh
# Builds deckhost's Linux packages for one architecture into $OUT/dist: a
# .deb, an .rpm and a tarball with install.sh (for per-user installs, e.g. on
# atomic distros). Runs in the build container from the repo root.
# Usage: package.sh ARCH PKG_VERSION BINARY
set -eu
ARCH=$1 PKG_VERSION=$2 bin=$3
OUT=${OUT:-build}
STAGE=$OUT/stage/linux-$ARCH
dist=$OUT/dist
rm -rf "$STAGE"
mkdir -p "$STAGE" "$dist"
cp "$bin" "$STAGE/deckhost"
for f in deckhost.service deckhost-tray.desktop; do
	sed 's|@BIN@|/usr/bin/deckhost|' "contrib/linux/$f" > "$STAGE/$f"
done
sed -e "s|\${ARCH}|$ARCH|g" -e "s|\${PKG_VERSION}|$PKG_VERSION|g" -e "s|\${STAGE}|$STAGE|g" \
	contrib/linux/nfpm.yaml > "$STAGE/nfpm.yaml"
for p in deb rpm; do
	.cache/bin/nfpm package -f "$STAGE/nfpm.yaml" -p "$p" -t "$dist/"
done

name=deckhost-$PKG_VERSION-linux-$ARCH
tree=$OUT/stage/$name
rm -rf "$tree"
mkdir -p "$tree"
cp "$bin" "$tree/deckhost"
cp contrib/linux/install.sh contrib/linux/deckhost.service contrib/linux/deckhost-tray.desktop \
	contrib/linux/70-d200-deck.rules LICENSE NOTICE.md "$tree/"
cp -r LICENSES "$tree/"
tar -C "$OUT/stage" -czf "$dist/$name.tar.gz" "$name"
