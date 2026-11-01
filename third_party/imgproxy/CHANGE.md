# imgproxy port changes

Record each upstream release in this file, newest first. Include applicable
patches, intentional exclusions, OSS compatibility, validation and rollback.
Keep commit-level decisions in REVIEWS.json and the source lock in UPSTREAM.json.
Do not create separate version-number Markdown files.

## v4.0.20 applicability and validation

Upstream target: 40db5f87cd15d64b5bfd35ac645185f075fa31b5.
All four commits are recorded in REVIEWS.json. An `adapted` decision identifies
an independent local implementation in the same release commit; unlike a
historical `ported` decision, it does not require a self-referential commit hash.

### Runtime security updates

imgproxy-base v4.0.12 updates Brotli, libheif, HarfBuzz and librsvg.
Alpine 3.23 supplies Brotli 1.2.1 but older libheif and HarfBuzz packages.
The Dockerfile builds checksum-pinned libheif 1.23.7 from its official source
archive, retaining the shared-library ABI used by libvips. HarfBuzz uses the
signed abuild package (15.0.0-r0). libheif includes libde265/x265 and
AOM/dav1d codecs.
Brotli 1.2.1 is required explicitly. Preserve the existing CA distrust floor.

References:
- https://github.com/google/brotli/releases/tag/v1.2.1
- https://github.com/strukturag/libheif/releases/tag/v1.23.7
- https://github.com/harfbuzz/harfbuzz/releases/tag/15.0.0

The libheif source archive is included in the source-export artifact and
licenses/version information in the runtime image. libheif is installed as
a genuine locally built APK. HarfBuzz now uses its own standard abuild stage
as recorded below.
The package database, ownership and exported inventory record installed versions.
The unused libheif plugin directory is removed. native-versions.txt
also records the source-built libheif version; harfbuzz-version.txt and alpine-packages.txt
record HarfBuzz.
This is not a comprehensive native package vulnerability audit.
librsvg remains Alpine-managed: SVG input processing is deliberately absent.
Pebble is absent. The only Go dependency, go-xmp, is unchanged by this release.

### 2026-11-02: build signed native APKs with abuild

Build libheif 1.23.7 and HarfBuzz 15.0.0 from official checksum-pinned release
sources using packaging/libheif/APKBUILD and packaging/harfbuzz/APKBUILD,
based on Alpine 3.23 recipes. Each library has a separate non-root Alpine SDK
stage. abuild applies distribution compiler settings, runs upstream tests,
generates dependency metadata and standard subpackages, and signs the APKs.

The Go stage installs matching libheif runtime/dev packages and HarfBuzz
core/dev/Cairo/GObject/ICU/subset/static/utility/document packages. The final
image installs only the required runtime packages. HarfBuzz's unused new
raster/vector/GPU backends are disabled. APK installation verifies signatures;
extra signing-key trust is removed afterward, without --allow-untrusted.
The native-export target exposes APKs and public keys. Source export includes
both recipes and matching native sources, with original licenses and no private
keys. This improves provenance and dependency resolution; it does not guarantee
a vulnerability scan with no findings.

Validation: libheif CTest had 39 passed and 12 conditionally skipped entries,
with no failures; all 241 HarfBuzz Meson tests passed. Exported APK signatures,
C header/link probes, header/runtime versions, automatic shared-library
dependencies and matching source/license checks passed. The final runtime has
no development packages or temporary signing keys. Full application regression
and ordinary/Race tests in the native SDK passed. Latin, Chinese, mixed and
missing-font fallback watermark samples matched the preceding image pixel for
pixel; these samples do not guarantee unchanged output for every font. Retain
the preceding image, configuration and matching source for rollback.

### OSS contract

Keep P_ in 1..100 and reject P_0/negative values. Omitting P_ keeps the watermark
at its native size. Do not import imgproxy's URL parser or expose its scale
argument. No change to application version, URL syntax or HTTP semantics.

### Validation and rollback

Run the project full regression, including ordinary/race tests, native ICO/BMP
ASAN checks and govulncheck. On the actual image verify loaded library versions,
HEIC/AVIF and other formats, Chinese/mixed watermarks, BYPASS, cache, signing,
ETag, SVG rejection, CA subjects and source-download integrity.

Retain the previous image digest, compose configuration and matching source.
On regression restore that image/configuration without deleting image data or
cache volumes; isolate processed caches across versions when output differs.
Revert this version commit independently. Rolling back native security fixes
requires restricting affected input formats until a fixed image is available.

## v4.0.19 applicability

All six upstream commits are recorded separately in REVIEWS.json.

The applicable trust-store change is already implemented in ca1ac3d:
both Docker stages require ca-certificates-bundle >= 20261020-r0.
Image regression checks the actual trusted subjects and rejects the three
removed emSign roots. Retain that behavior and the existing Alpine runtime.

Ubuntu migration, AWS Lambda adapter, allocator entrypoint, cloud SDKs and
upstream release version do not map to local runtime components.
The local go-xmp dependency is unchanged upstream. No OSS behavior changes.

Verification: full server regression passed, including native bounds tests.
Rollback: reverting this record must not remove the existing CA package floor.

## v4.0.18 applicability

Baseline: v4.0.17. All 12 upstream commits are recorded in REVIEWS.json.

- BMP RLE8 row-buffer padding fix is already in ca1ac3d, including pixel and
  truncation regressions. Keep the 256-byte minimum and local cleanup.
- ASAN coverage is already in CI. The upstream DCT test helper is absent.
- SVG/XML sanitization and remote Content-Disposition code are absent.
  Preserve SVG rejection and local fixed source-download filename.
- Cloud SDK updates, upstream test harness and Ubuntu base are not imported.
- OSS URL syntax, status codes and watermark semantics stay unchanged.

Verification: source lock, maintenance-tool tests and server regression.
Rollback: revert this record only; do not undo existing BMP security fixes.
