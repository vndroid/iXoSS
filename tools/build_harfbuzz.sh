#!/bin/sh
# Keep HarfBuzz packaging independent of the cached libheif build.
set -eu
cd /home/builder/aports/main/harfbuzz
abuild-keygen -a -i -n -b 4096
doas apk update
abuild -r -K -P /home/builder/packages -s /home/builder/distfiles
. ./APKBUILD
carch=$(abuild -A)
cp /home/builder/packages/main/"$carch"/harfbuzz-*.apk /harfbuzz-apks/
cp /home/builder/.abuild/*.rsa.pub /harfbuzz-keys/
cp "/home/builder/distfiles/$pkgname-$pkgver.tar.xz" /harfbuzz-src/harfbuzz.tar.xz
cp "src/$pkgname-$pkgver/COPYING" /harfbuzz-licenses/LICENSE-HarfBuzz
printf 'harfbuzz %s (abuild)\n' "$pkgver" > /harfbuzz-licenses/harfbuzz-version.txt
