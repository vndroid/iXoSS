# Distribution notices and corresponding source

The project license is `LICENSE` (AGPL-3.0). Imported imgproxy code retains
Apache-2.0 terms in `LICENSE-imgproxy`. The original upstream `NOTICE` is
preserved without edits in `third_party/imgproxy/NOTICE.upstream`; its inventory
describes upstream's build, including packages this port does not use.

Additional original notices are retained here:

- `LICENSE-go-xmp`: github.com/trimmer-io/go-xmp v1.0.0, Apache-2.0.
- `LICENSE-Go` and `PATENTS-Go`: the Go toolchain's standard-library license
  and patent grant. Docker copies the actual builder's versions into the image.
- `WenQuanYi-COPYING`, `WenQuanYi-AUTHORS`, `WenQuanYi-README`: unchanged files
  from WenQuanYi Zen Hei 0.9.46. Alpine's package identifies the font license
  as GPL-2.0-only; its original README states a font-embedding exception.
  The original documents are preserved as received. The font is a
  separate runtime file, not linked into the program. Original archive:
  https://distfiles.alpinelinux.org/distfiles/v3.23/font-wqy-zenhei-0.9.46.tar.bz2
  SHA-512: `ff41473fc9bd69c04cc509d1f8915d5bcf99d175ed9dfd2f9627fd56f6a4e7720e4930d35313d1e89f090b229f2e3f461c948046257fadee192dd618471f7833`.

## Project source offer

The Docker build packages the actual build inputs, including uncommitted source
changes, licenses, build instructions and the vendored Go module, into
`/usr/share/doc/ixoss/source.tar.gz`. It excludes Git data, private configuration,
images, caches and build artifacts. The archive is created before compilation
from the same source tree used to build the executable.

The image sets `IXOSS_SOURCE_ARCHIVE` to that file. HTTP responses advertise
`/_ixoss/source.tar.gz` through a `Link` header; GET/HEAD of that endpoint offer
the archive without an image-request signature. Do not block this route in a
reverse proxy when relying on it as the source offer.

For a standalone binary, provide a trusted readable archive through
`IXOSS_SOURCE_ARCHIVE`, or an absolute HTTP(S) `IXOSS_SOURCE_URL` pointing to the
complete corresponding source for that specific build. An archive takes
precedence. Both are unset by default outside the Docker image; operators must
arrange the offer where the license requires it. A link to a moving branch is
not a substitute for source matching the deployed binary.

After extracting the Docker source archive, a compatible Go/libvips development
environment can build with `CGO_ENABLED=1 go build -mod=vendor ./cmd/ixoss`.
The Dockerfile describes the Alpine environment. Preserve the accompanying
license documents when distributing an extracted binary.

## Native runtime packages

The runtime image also includes `alpine-packages.txt` and
`alpine-package-metadata.txt` in `/usr/share/doc/ixoss/`. The latter preserves
Alpine's installed package metadata, including versions, origins, licenses and
aports commit references. These files identify the actual image contents;
the historical imgproxy inventory must not be used as that inventory.

Alpine's packaging source is https://gitlab.alpinelinux.org/alpine/aports;
release-branch recipes and recorded package commits identify source archives
and checksums. Alpine source mirrors are at
https://distfiles.alpinelinux.org/distfiles/v3.23/.
Go source matching the actual builder version is available at https://go.dev/dl/.

The project source archive contains iXoSS and its vendored Go module, not every
Alpine library or the toolchain. When redistributing a runtime image, also meet
each native package's license/source obligations using the actual package
inventory and preserve additional relevant copyright notices. Dynamic linking
does not remove LGPL distribution obligations. For GPL/LGPL components outside
a system-library exception, arrange a compliant corresponding-source offering;
do not treat a general upstream homepage as a source offer or warranty that
all package obligations have been discharged.
