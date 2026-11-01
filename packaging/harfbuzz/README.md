# Alpine-style HarfBuzz package

The recipe adapts Alpine 3.23's HarfBuzz APKBUILD to the official 15.0.0 release:
https://github.com/alpinelinux/aports/blob/3.23-stable/main/harfbuzz/APKBUILD

The source tarball is pinned by SHA-512. abuild-meson applies distribution
compiler settings; the recipe enables GLib, GObject, FreeType, Graphite2, ICU,
Cairo, introspection, documentation and the upstream test suite. New optional
raster/vector/GPU libraries are disabled because this service does not use them.
Runtime, development, static, Cairo, GObject, ICU, subset, utility and document
packages follow the distribution's split layout and automatic dependencies.

The isolated non-root `harfbuzz-native` SDK stage builds and signs the packages
independently of libheif. A distinct per-build signing identity prevents key
filename collisions. Only public keys and packages are exported; private keys
stay in the native build stage. Go installs the matching development and
companion packages. The final image installs only the core runtime package,
with other shared-library dependencies resolved by APK. Temporary trust keys
are removed after verified installation, without `--allow-untrusted`.

Export both native libraries' signed packages and public keys:

```sh
docker buildx build --target native-export \
  --output type=local,dest=./dist/native .
```

The source-export archive includes this recipe, tools/build_harfbuzz.sh and the
matching HarfBuzz tarball. Runtime packages and exported source include the
original license. These are iXoSS-built packages, not official Alpine updates.
Standard packaging does not guarantee a vulnerability scan with no findings.

Acceptance checks cover upstream Meson tests, APK signatures and package
ownership, development-header/runtime agreement, shared-library resolution,
Latin/Chinese/mixed/fallback watermark rendering, and full application/image
regression. Retain the preceding image for rollback. Long-lived release signing
requires a protected key and a corresponding signing workflow; do not publish
the native-stage cache as a signing-key store.
