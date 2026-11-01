#!/bin/sh
# Run as the unprivileged APK builder in the isolated native Docker stage.
set -eu
cd /home/builder/aports/main/libheif
# Keys are ephemeral build artifacts. Only public keys leave this stage.
abuild-keygen -a -i -n -b 4096
# The SDK layer uses --no-cache; abuild needs verified repository indexes.
doas apk update
abuild -r -K -P /home/builder/packages -s /home/builder/distfiles

# Use the recipe's version for the exported package and corresponding source.
. ./APKBUILD
carch=$(abuild -A)
cp "/home/builder/packages/main/$carch/$pkgname-$pkgver-r$pkgrel.apk" /native-apks/
cp "/home/builder/packages/main/$carch/$pkgname-dev-$pkgver-r$pkgrel.apk" /native-apks/
cp /home/builder/.abuild/*.rsa.pub /native-keys/
cp "/home/builder/distfiles/$pkgname-$pkgver.tar.gz" /native-src/libheif.tar.gz
cp "src/$pkgname-$pkgver/COPYING" /native-licenses/LICENSE-libheif
printf 'libheif %s (abuild)\n' "$pkgver" > /native-licenses/native-versions.txt
