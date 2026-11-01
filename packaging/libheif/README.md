# Alpine-style libheif package

The APKBUILD follows the Alpine 3.23 recipe:
https://github.com/alpinelinux/aports/blob/3.23-stable/main/libheif/APKBUILD

It selects the official libheif 1.23.7 release tarball with a fixed SHA-512.
The service retains libde265/x265, AOM and dav1d support and disables unused
CLI programs and pixbuf/dynamic plugins. The enabled upstream CTest suite runs
in check(); tests for disabled codecs or hidden internal APIs may be skipped.

Docker's isolated `native` stage runs abuild as a non-root builder. abuild uses
Alpine compiler settings, checks sources, generates SONAME/pkg-config dependency
metadata, splits the runtime and development packages, and signs the APKs.
An ephemeral private key stays in that stage. Only APKs, the public key, license
and source archive are copied out. Do not publish the native-stage build cache
as a signing-key store; long-lived release signing would require a protected persistent key supplied
through a build secret and a corresponding signing workflow.

The Go build stage installs both libheif and libheif-dev from this build. The
runtime installs only libheif and uses Alpine packages for its shared-library
dependencies. HarfBuzz uses its own signed abuild package; see the sibling
[HarfBuzz recipe](../harfbuzz/README.md). Installation verifies the APK signature using the
copied public key; that extra trust key is removed afterward. Normal Alpine
repository keys remain unchanged. No `--allow-untrusted` is needed.

Export packages independently of the application:

```sh
docker buildx build --target native-export \
  --output type=local,dest=./dist/libheif .
```

The result contains `apks/` and `keys/`. Trust the public key from the same
verified build when installing these local APKs. It is an iXoSS-built package,
not an officially published Alpine update. Package metadata consistency and
automatic dependency resolution do not guarantee that every vulnerability
scanner will accept the image; evaluate actual findings against the installed
versions and upstream advisories.

The source-export target includes the recipe, build script and matching release
source. Neither application images nor exported artifacts contain the private
signing key. Rebuild and test when changing Alpine, compilers or native packages.
