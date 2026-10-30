# imgproxy port changes

Record each upstream release in this file, newest first. Include applicable
patches, intentional exclusions, OSS compatibility, validation and rollback.
Keep commit-level decisions in REVIEWS.json and the source lock in UPSTREAM.json.
Do not create separate version-number Markdown files.

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
