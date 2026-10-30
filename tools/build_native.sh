#!/bin/sh
# Security backports corresponding to imgproxy-base v4.0.12.
set -eu
mkdir -p /native-src /native-root /native-licenses
cd /native-src
curl -fL --retry 3 https://github.com/strukturag/libheif/archive/refs/tags/v1.23.7.tar.gz -o libheif.tar.gz
curl -fL --retry 3 https://github.com/harfbuzz/harfbuzz/archive/refs/tags/15.0.0.tar.gz -o harfbuzz.tar.gz
printf '%s\n' \
 '90e389caad285ca4946461d56d1c115e56bcbb5141d9038050282af91064fbc7  libheif.tar.gz' \
 'fe4b8bad7afbbae75d61b2095c267c98f4ed04fe4a117680130ed253d2732efe  harfbuzz.tar.gz' | sha256sum -c -
tar -xzf libheif.tar.gz
tar -xzf harfbuzz.tar.gz
cmake -S libheif-1.23.7 -B heif-build -G Ninja \
 -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=/usr \
 -DCMAKE_INSTALL_LIBDIR=lib -DBUILD_TESTING=OFF -DBUILD_DOCUMENTATION=OFF \
 -DWITH_EXAMPLES=OFF -DWITH_GDK_PIXBUF=OFF -DENABLE_PLUGIN_LOADING=OFF \
 -DWITH_DAV1D=ON -DWITH_DAV1D_PLUGIN=OFF
cmake --build heif-build --parallel 2
DESTDIR=/native-root cmake --install heif-build
meson setup hb-build harfbuzz-15.0.0 --prefix=/usr --libdir=lib \
 --buildtype=release -Dtests=disabled -Ddocs=disabled -Dutilities=disabled \
 -Dintrospection=disabled -Dglib=enabled -Dgobject=enabled -Dfreetype=enabled \
 -Dicu=enabled -Draster=disabled -Dvector=disabled -Dgpu=disabled
meson compile -C hb-build -j 2
DESTDIR=/native-root meson install -C hb-build
cp libheif-1.23.7/COPYING /native-licenses/LICENSE-libheif
cp harfbuzz-15.0.0/COPYING /native-licenses/LICENSE-harfbuzz
printf 'libheif 1.23.7\nharfbuzz 15.0.0\n' > /native-licenses/native-versions.txt
