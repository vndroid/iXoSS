FROM alpine:3.23 AS native
RUN apk add --no-cache alpine-sdk doas 'ca-certificates-bundle>=20261020-r0' \
    && adduser -D builder \
    && addgroup builder abuild \
    && mkdir -p /etc/doas.d /home/builder/aports/main/libheif \
        /native-apks /native-keys /native-src /native-licenses \
    && echo 'permit nopass :abuild' > /etc/doas.d/abuild.conf \
    && chown -R builder:builder /home/builder /native-apks /native-keys /native-src /native-licenses
COPY --chown=builder:builder packaging/libheif/APKBUILD /home/builder/aports/main/libheif/APKBUILD
COPY tools/build_native.sh /build_native.sh
USER builder
ENV PACKAGER="iXoSS build <build@ixoss.invalid>" JOBS=2
RUN sh /build_native.sh

# A separate SDK stage keeps HarfBuzz changes from rebuilding libheif.
FROM alpine:3.23 AS harfbuzz-native
RUN apk add --no-cache alpine-sdk doas 'ca-certificates-bundle>=20261020-r0' \
    && adduser -D builder \
    && addgroup builder abuild \
    && mkdir -p /etc/doas.d /home/builder/aports/main/harfbuzz \
        /harfbuzz-apks /harfbuzz-keys /harfbuzz-src /harfbuzz-licenses \
    && echo 'permit nopass :abuild' > /etc/doas.d/abuild.conf \
    && chown -R builder:builder /home/builder /harfbuzz-apks /harfbuzz-keys /harfbuzz-src /harfbuzz-licenses
COPY --chown=builder:builder packaging/harfbuzz/APKBUILD /home/builder/aports/main/harfbuzz/APKBUILD
COPY tools/build_harfbuzz.sh /build_harfbuzz.sh
USER builder
ENV PACKAGER="iXoSS HarfBuzz build <harfbuzz-build@ixoss.invalid>" JOBS=2
RUN sh /build_harfbuzz.sh

# Export the signed APKs and public verification key independently of the app.
FROM scratch AS native-export
COPY --from=native /native-apks/ /apks/
COPY --from=native /native-keys/ /keys/
COPY --from=harfbuzz-native /harfbuzz-apks/ /apks/
COPY --from=harfbuzz-native /harfbuzz-keys/ /keys/

FROM golang:1.27-alpine3.23 AS build
COPY --from=harfbuzz-native /harfbuzz-apks/ /tmp/harfbuzz-apks/
COPY --from=native /native-apks/ /tmp/native-apks/
COPY --from=native /native-keys/ /tmp/native-keys/
COPY --from=harfbuzz-native /harfbuzz-keys/ /tmp/native-keys/
RUN cp /tmp/native-keys/*.pub /etc/apk/keys/ \
    && apk add --no-cache 'ca-certificates-bundle>=20261020-r0' \
        build-base pkgconf vips-dev vips-heif vips-jxl \
        /tmp/native-apks/libheif-[0-9]*.apk /tmp/native-apks/libheif-dev-*.apk \
        /tmp/harfbuzz-apks/*.apk \
    && for key in /tmp/native-keys/*.pub; do rm "/etc/apk/keys/$(basename "$key")"; done \
    && rm -rf /tmp/native-apks /tmp/native-keys /tmp/harfbuzz-apks
COPY --from=native /native-src/libheif.tar.gz /out/native-source/libheif.tar.gz
COPY --from=harfbuzz-native /harfbuzz-src/harfbuzz.tar.xz /out/native-source/harfbuzz.tar.xz
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY tools/ ./tools/
COPY packaging/ ./packaging/
COPY third_party/ ./third_party/
COPY LICENSE LICENSE-imgproxy NOTICE README.md DIFF.md Dockerfile compose.example.yaml ./
RUN go mod vendor \
    && mkdir -p /out/licenses \
    && cp /usr/local/go/LICENSE /out/licenses/LICENSE-Go \
    && cp /usr/local/go/PATENTS /out/licenses/PATENTS-Go \
    && cp -r /out/native-source ./native-source \
    && tar -czf /out/source.tar.gz native-source cmd internal vendor tools packaging third_party \
        go.mod go.sum LICENSE LICENSE-imgproxy NOTICE README.md DIFF.md Dockerfile compose.example.yaml
ARG CHANNEL=DEVELOPMENT
ARG BUILD_DATE=""
ARG REVISION=unknown
RUN build_date="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}" \
    && CGO_ENABLED=1 go build -trimpath \
        -ldflags="-s -w -X main.Channel=${CHANNEL} -X main.BuildDate=${build_date} -X main.Revision=${REVISION}" \
        -o /out/ixoss ./cmd/ixoss

# Export matching source as a release asset, separate from the runtime image.
FROM scratch AS source-export
COPY --from=build /out/source.tar.gz /ixoss-source.tar.gz

FROM alpine:3.23 AS runtime
COPY --from=harfbuzz-native /harfbuzz-apks/harfbuzz-[0-9]*.apk /tmp/harfbuzz-apks/
ARG IXOSS_SOURCE_URL=""

COPY --from=native /native-apks/libheif-[0-9]*.apk /tmp/native-apks/
COPY --from=native /native-keys/ /tmp/native-keys/
COPY --from=harfbuzz-native /harfbuzz-keys/ /tmp/native-keys/
# Trust this build's key only for installation; retain the normal Alpine keys.
RUN cp /tmp/native-keys/*.pub /etc/apk/keys/ \
    && apk add --no-cache 'ca-certificates-bundle>=20261020-r0' \
        'brotli>=1.2.1-r0' 'libexpat>=2.10.0-r0' vips vips-heif vips-jxl font-wqy-zenhei \
        /tmp/native-apks/libheif-[0-9]*.apk /tmp/harfbuzz-apks/harfbuzz-[0-9]*.apk \
    && for key in /tmp/native-keys/*.pub; do rm "/etc/apk/keys/$(basename "$key")"; done \
    && rm -rf /tmp/native-apks /tmp/native-keys /tmp/harfbuzz-apks \
    && addgroup -S -g 10001 ixoss \
    && adduser -S -D -H -u 10001 -G ixoss ixoss \
    && mkdir -p /srv/images /var/cache/ixoss \
    && chown ixoss:ixoss /srv/images /var/cache/ixoss

COPY --from=native /native-licenses/ /usr/share/doc/ixoss/licenses/
COPY --from=harfbuzz-native /harfbuzz-licenses/ /usr/share/doc/ixoss/licenses/
COPY --from=build /out/ixoss /usr/local/bin/ixoss
COPY LICENSE LICENSE-imgproxy NOTICE /usr/share/doc/ixoss/
COPY third_party/imgproxy/ /usr/share/doc/ixoss/imgproxy/
COPY third_party/licenses/ /usr/share/doc/ixoss/licenses/
COPY --from=build /out/licenses/ /usr/share/doc/ixoss/licenses/
RUN apk --no-cache info -vv > /usr/share/doc/ixoss/alpine-packages.txt \
    && cp /lib/apk/db/installed /usr/share/doc/ixoss/alpine-package-metadata.txt

ENV IXOSS_ROOT=/srv/images \
    IXOSS_CACHE_DIR=/var/cache/ixoss \
    IXOSS_BIND=0.0.0.0:8080 \
    IXOSS_SOURCE_URL=${IXOSS_SOURCE_URL}

USER ixoss:ixoss
EXPOSE 8080
ENTRYPOINT ["ixoss"]
