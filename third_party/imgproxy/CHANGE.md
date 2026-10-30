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
The Dockerfile therefore builds checksum-pinned libheif 1.23.7 and HarfBuzz
15.0.0 from official source archives, retaining the shared-library ABI used by
libvips and Pango. libheif includes libde265/x265 and AOM/dav1d codecs.
Brotli 1.2.1 is required explicitly. Preserve the existing CA distrust floor.

References:
- https://github.com/google/brotli/releases/tag/v1.2.1
- https://github.com/strukturag/libheif/releases/tag/v1.23.7
- https://github.com/harfbuzz/harfbuzz/releases/tag/15.0.0

The native source archives are included in the source-export artifact and
licenses/version information in the runtime image. Alpine's package inventory
still describes installed distribution packages; native-versions.txt records
the replacement libraries. Check actual loaded library versions when scanning.
This is not a comprehensive native package vulnerability audit.
librsvg remains Alpine-managed: SVG input processing is deliberately absent.
Pebble is absent. The only Go dependency, go-xmp, is unchanged by this release.

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
