# imgproxy port changes

Record each upstream release in this file, newest first. Include applicable
patches, intentional exclusions, OSS compatibility, validation and rollback.
Keep commit-level decisions in REVIEWS.json and the source lock in UPSTREAM.json.
Do not create separate version-number Markdown files.

## v4.0.22 applicability and validation

Upstream target: a08006d11003eef87218a7407b1f21a40f6da615. Type: bug-fix release
("Minor bugs in bmp image loader") plus dependency updates. No source change is
a vulnerability fix; the one security item is libexpat in the base image. All
five commits are in REVIEWS.json. The interval stays incomplete until the BMP
fixes below are decided.

### libexpat floor (applied)

imgproxy-base v4.0.14 moves to libexpat 2.10.0, which fixes CVE-2026-113351,
CVE-2026-115506, CVE-2026-117610 and CVE-2026-118370. Alpine 3.23 already ships
2.10.0-r0; the runtime stage now requires it, as it does Brotli and the CA
bundle, so a cached layer cannot keep an older package. No request reaches
expat here: libvips uses it only for its `.v` format, which iXoSS never loads,
and fontconfig only for local configuration files.

### BMP correctness fixes (not ported, decision pending)

`823aa6b` is not a security fix. The upstream samples show the same behavior in
this port, with no ASAN report on any of the four:

| Input | Current result | Upstream result |
| --- | --- | --- |
| 16-bit BI_RGB (RGB555), white | black | near-white |
| 16-bit BITFIELDS 5-5-5, white | `787cf8` | near-white |
| RLE8 MOVE_TO with dy > 0 | rows one too far down | correct rows |

Its NULL checks in bmpload.c and bmpsave.c cannot trigger: `VIPS_MALLOC` is
`g_malloc0`, which aborts rather than returns NULL. It also zeroes the buffer,
so indices past a truncated palette read zeros, not heap data. Porting would
change the output of affected BMPs, and processed-cache entries for them would
need clearing because the cache key does not cover decoder fixes.

### Not applicable

- HarfBuzz stays at 15.0.0. The 15.1.0 crash and hang fixes concern malformed
  fonts; fonts come from the image, never from a request. Its raster, vector
  and GPU libraries are disabled here.
- go-jose and the other Go updates belong to removed cloud code; go-xmp is
  unchanged. Upstream workflow changes do not apply.

### OSS contract and rollback

No change to URL syntax or HTTP behavior. Reverting the floor is safe only while
the Alpine repository still resolves libexpat to 2.10.0 or later.

## v4.0.21 applicability and validation

Upstream target: 302105ccd183d0038ef71aa11ab4ff3de5f0c989. Type: security
release ("Fixed several vulnerabilities in BMP and ICO loaders") plus
dependency updates. Both commits are recorded in REVIEWS.json; the interval
is complete.

### BMP/ICO loader fixes (ported in 4f13fb5)

`a0bbb32` (GHSA-c4f2-mxv4-6fp5) is ported to internal/vips:

- bmpload.c: width and height are capped at 1,000,000 and the pixel area at
  INT_MAX/4 before any row-size arithmetic; an INT32_MIN height is rejected
  before negation; row sizes use size_t; strip-geometry `g_assert`s become
  errors, so a malformed BMP fails one request instead of aborting the process.
- icoload.c: the embedded BMP must have a 40-byte BITMAPINFOHEADER, its palette
  must fit the entry, and the synthesized header is written with memcpy.
- bmpsave.c checks the palette write; source.go keeps bytes returned together
  with io.EOF; vips.go rejects blobs over 2 GiB and ignores empty blob writes.

The port already parenthesized `MAX_ICO_ENTRY_DATA_SIZE` and keeps its g_free
cleanup. Upstream's binary fixtures are rebuilt in code from their headers.
The deleted bpp=40 integration test corresponds to 60d367c, already present;
a unit regression now pins it.

Evidence against the unpatched code: 10 of 11 new cases failed. Overflowing
widths and an INT32_MIN height were accepted; a 100000x100000 header made
ASAN abort on a 28 GiB allocation; a 1000001-row bottom-up BMP aborted the
whole process on an assertion. With the fix all cases pass, including under
Clang ASAN, together with the full server regression.

### Native library updates (assessed, not built)

imgproxy-base v4.0.13 updates four libraries that Alpine 3.23 ships older.
Each was checked against the functions this image actually calls (library
linkage and imported symbols in Alpine 3.23 vips, libheif plugin priorities).
Decision on 2026-11-03: none is built for now.

| Library | Alpine / upstream | Reached through | Fixes on that path | Decision |
| --- | --- | --- | --- | --- |
| libjpeg-turbo | 3.1.2 / 3.2.1 | libvips, every JPEG | None. libvips uses only the classic libjpeg API (`jpeg_read_scanlines` etc.); the fixes, including CVE-2026-75466, are in TurboJPEG `tj3*`, the Java wrapper, jpegtran, `jpeg_crop_scanline`/`jpeg_skip_scanlines` or API misuse | Not applicable |
| fribidi | 1.0.16 / 1.0.18 | Pango, text watermarks | Pango calls only `fribidi_get_par_embedding_levels_ex`, `fribidi_get_bidi_type` and `fribidi_get_bracket`. The out-of-bounds write (`fribidi_remove_bidi_marks`) and unchecked allocations (`reorder_line`, `log2vis`) are not called. The quadratic time on many isolates is reachable from `text_` | Mitigated by the 14-column text cap (899d7e6) |
| lcms2 | 2.19 / 2.20 | libvips, embedded ICC profiles of source images | Profile I/O double-free guards (`153fd047`) and float CLUT interpolation bounds (`8db7d032`); libvips opens embedded profiles and builds float transforms | Risk accepted: source images are maintained by the operator |
| aom | 3.14.1 / 3.15.3 | libheif, AVIF encoding only (dav1d, priority 150, decodes AVIF ahead of aom's 100) | 3.15.0 fixed a multithreaded-encoding double free; libheif encodes with one thread per CPU. The 3.15.3 overflow is in realtime partitioning, not libheif's default good-quality mode | Risk accepted: encoded pixels come from operator-maintained images |

Revisit lcms2 and aom when any of these changes:

- source images can be supplied by users or other untrusted parties;
- Alpine publishes these versions (re-check at each Alpine package update);
- a fix is published with a known exploit for a path listed above.

Enabling request signing (`IXOSS_KEY`/`IXOSS_SALT`) further limits aom exposure:
unsigned requests can force AVIF encoding of any source at chosen dimensions.
The assessment follows call paths and release notes; no exploit was built for
each fix, and aom 3.15.0's security fixes are only partly described upstream.
This is not a comprehensive native vulnerability audit.
The Go module updates affect removed cloud/telemetry code; go-xmp is unchanged.

### OSS contract

No change to URL syntax, status codes or response bodies for valid images.
Malformed BMP and ICO inputs that were previously accepted, or crashed the
process, now fail with the normal processing error. Separately, 899d7e6 caps
`watermark,text_` at 7 CJK or 14 other characters (400 above that); OSS
documents no such limit, see DIFF.md.

### Rollback

Revert 4f13fb5 together with this record. Do not roll back the loader fixes
without restricting BMP and ICO input, because the assertion failure is a
process-level denial of service.

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
