# imgproxy port baseline

The audited release baseline is **v4.0.17**, commit
`2c89e4ebff3384caf6752db2ec861b4ef5fe675c`.
The machine-readable version marker is [UPSTREAM.json](UPSTREAM.json).
This is a maintained source port, not an imgproxy module dependency or an
unmodified copy of that release. It does not set iXoSS's release version.

## Provenance and comparison

The original import did not record a tag or commit. Its exact historical origin
cannot be recovered uniquely from the adapted files. The previous description
as approximately v4.0.14/v4.0.15 was incomplete: the port also contains master
changes absent from those releases.

The audit on 2026-10-22 compared every non-test Go/C/header file in the 13
derived directories listed in NOTICE. Only the import prefix was normalized
from `github.com/vndroid/iXoSS/internal/` to
`github.com/imgproxy/imgproxy/v4/`; comments, whitespace and code were not
normalized. Against v4.0.17, 51 files match exactly after that rewrite, 31
contain modifications, and 3 have no upstream counterpart. UPSTREAM.json
records the local and upstream SHA-256 values and classification for each file.
[PORT.diff](PORT.diff) contains the complete normalized source differences
(a unified diff without context lines),
including the three local-only files. It is comparison evidence; module-path
rewriting is intentionally absent from that diff.
Tests are excluded from the source count, not from verification.

Ported files now carry a common iXoSS modification notice. Raw local hashes
include it; the comparison removes only that exact prefix before the import
rewrite. Original upstream notices and other comments are not removed.
`NOTICE.upstream` retains the original upstream NOTICE verbatim, with its hash
recorded in the lock. Its dependency list is historical, not our image inventory.

For the same audited directories, v4.0.14 and v4.0.15 have identical source.
Between v4.0.15 and v4.0.17 the only production-source changes in those
directories are the two ICO security fixes listed below. Changes to upstream
dependencies and Docker base images are not source patches to this port:
iXoSS has its own go.mod and Alpine runtime, which need separate maintenance.

An additional comparison used master commit
`afd17ddbc8d8b24cdfd276b68a1064d28aaed261`: 58 of the 82 common files matched
before this backport. Six files differing from the release instead match that
master snapshot. Examples of already present master changes are:

| Commit | Already present feature |
| --- | --- |
| `89ef08ed2728f176289aa339bb5599a9825a98bc` | LogValue methods in image/gravity/resize types |
| `faf382077474eee3c406fa85e8007d340619be12` | TIFF unlimited option; iXoSS detects availability at runtime |
| `3efedd9fb5bc18a8686fbf807826b3791fd13ed1` | Auxiliary image provider Close lifecycle |

These are comparison evidence, not proof that the complete commits were
cherry-picked or that every master change was imported.

## Intentional differences

The 31 modified files fall into these groups; the per-file list is in the lock:

| Area | Differences retained by iXoSS |
| --- | --- |
| processing (9 files), options/keys | OSS-specific operations/options, arbitrary rotation, brightness/contrast, rounded shapes, output format/animation handling, removal of SVG processing, local timeout API and error handling |
| vips (10 files) | Additional C operations, runtime encoder/load-option probes, per-request save overrides, decoupling from upstream imagedata/options, configuration/error adaptation, ICO buffer cleanup and the RLE8 padding bound |
| security (4 files) | IXOSS configuration and limit validation, original-header checks, signing helper and adapted errors |
| auximageprovider (3 files) | Close interface, local-only watermark source and IXOSS configuration |
| env (3 files), imagetype/type | Removal of cloud-config support, IXOSS prefix handling and master logging/type changes |

The three local-only files are processing/adjust.go, rotate_free.go and
round_corners.go. The rewritten internal/imagedata and internal/timeout are
outside the 13 copied directory trees. Removed upstream packages (including
cloud storage, monitoring, URL parser and SVG processing), HTTP/OSS front-end
code and new tests are not described as identical upstream files. See the
root README's "What was changed relative to imgproxy" for the functional detail.

## Security patches included

| Upstream release / commit | Ported behavior |
| --- | --- |
| v4.0.16 / `a821b6a4cb07a1bf2bb9077431d1c1e734d79917` | Reject declared ICO entries above 10 MiB before allocating; check allocation failure; free buffers on failures |
| v4.0.17 / `39fd9e844a8a46084161502e993d5a3c16ccf28d` | Require a full 40-byte BMP payload header, excluding the synthesized 14-byte BMP file header |
| v4.0.18 / `9d8bf3e54b8665a98c5934df15e7d0a31c5934e2` | Reserve 256 bytes for the longest RLE8 absolute run: 255 pixel bytes plus one word-alignment padding byte |

The v4.0.16 length check alone was insufficient. The port uses the corrected
v4.0.17 payload check directly and retains its existing g_free cleanup.
The size cap also prevents overflowing `data_size + BMP_FILE_HEADER_LEN`.
Regression tests exercise oversized and overflowing declarations, truncated
headers (including the 26..39-byte gap), and valid PNG/BMP ICO decoding and
pixel output. Allocation failure is guarded in C but is not fault-injected.

Earlier BMP fixes (`6f56c3a1dcc5f963ee527b8a9ebd0b2bcae6ca33` and
`60d367c8deb20aa514afeb8f7b21cef7e53085ed`) are already present:
their fixes remain present; bmpload.c now differs from the pinned release by
the additional RLE8 padding bound. New tests force lazy decoding for narrow
and full-width odd/even runs, check decoded pixels and reject truncated input.
CI runs the ICO/BMP regression cases with Clang ASAN. Leak detection is disabled
because of unrelated fontconfig reports; this is a bounds check, not a leak audit.

## Reproducing the audit

Use a clean imgproxy checkout at the exact baseline commit:

```sh
git clone https://github.com/imgproxy/imgproxy.git ../imgproxy-reference
git -C ../imgproxy-reference checkout --detach 2c89e4ebff3384caf6752db2ec861b4ef5fe675c
python3 tools/audit_imgproxy.py --upstream ../imgproxy-reference
```

Without --upstream, the command checks local file hashes against the recorded
audit; CI runs this check so changes to the port require updating its record.
With --upstream it also compares upstream hashes and classifications.
It also verifies release tags, the complete reviewed commit interval and its
changed-file lists against that checkout. Offline CI validates record structure,
source hashes, notice integrity, counts and pending/completed review consistency;
it cannot prove that a human applicability decision is correct.
After reviewing an intentional source change, add --write to regenerate the
file audit. A future baseline update must explicitly change the pinned version,
commit, patch history and this document; --write does not choose a new version.

## Release review records

`UPSTREAM.json` separates the fixed source baseline from review progress and
maps copied files and rewritten/removed components to their local counterparts.
`REVIEWS.json` records every commit in v4.0.15..v4.0.17, including dependencies,
applicability reasons and validation evidence for the two adopted ICO patches.
Earlier BMP fixes are recorded as prior evidence, not a completed older review
interval. This is not a comprehensive audit of older history or native packages.

To prepare a later release review using a temporary upstream checkout:

```sh
python3 tools/review_imgproxy.py --upstream ../imgproxy-reference \
  --from-ref v4.0.17 --to-ref NEW_RELEASE > review-candidates.json
python3 -m unittest discover -s tools -p 'test_*.py'
```

Replace NEW_RELEASE with the selected tag. The report resolves full SHAs and
includes every commit, even unmapped files, without applying patches or deciding
applicability. Review release notes, complete diffs, callers and dependencies.
Use `ported`, `already_present`, `not_applicable`, `pending_analysis`,
`pending_port` or `pending_validation`, recording reasons and regression evidence.
Keep an interval incomplete while any decision is pending. Updating the review
cursor does not change the source baseline or make a partial port a pure release.
