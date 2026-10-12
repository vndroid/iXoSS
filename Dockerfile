FROM golang:1.26-alpine3.23 AS build

RUN apk add --no-cache build-base pkgconf vips-dev vips-heif vips-jxl
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY tools/ ./tools/
COPY third_party/ ./third_party/
COPY LICENSE LICENSE-imgproxy NOTICE README.md DIFF.md Dockerfile compose.example.yaml ./
RUN go mod vendor \
    && mkdir -p /out/licenses \
    && cp /usr/local/go/LICENSE /out/licenses/LICENSE-Go \
    && cp /usr/local/go/PATENTS /out/licenses/PATENTS-Go \
    && tar -czf /out/source.tar.gz cmd internal vendor tools third_party \
        go.mod go.sum LICENSE LICENSE-imgproxy NOTICE README.md DIFF.md Dockerfile compose.example.yaml
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/ixoss ./cmd/ixoss

FROM alpine:3.23

RUN apk add --no-cache vips vips-heif vips-jxl font-wqy-zenhei \
    && addgroup -S -g 10001 ixoss \
    && adduser -S -D -H -u 10001 -G ixoss ixoss \
    && mkdir -p /srv/images /var/cache/ixoss \
    && chown ixoss:ixoss /srv/images /var/cache/ixoss

COPY --from=build /out/ixoss /usr/local/bin/ixoss
COPY LICENSE LICENSE-imgproxy NOTICE /usr/share/doc/ixoss/
COPY third_party/imgproxy/ /usr/share/doc/ixoss/imgproxy/
COPY third_party/licenses/ /usr/share/doc/ixoss/licenses/
COPY --from=build /out/licenses/ /usr/share/doc/ixoss/licenses/
COPY --from=build /out/source.tar.gz /usr/share/doc/ixoss/source.tar.gz
RUN apk info -vv > /usr/share/doc/ixoss/alpine-packages.txt \
    && cp /lib/apk/db/installed /usr/share/doc/ixoss/alpine-package-metadata.txt

ENV IXOSS_ROOT=/srv/images \
    IXOSS_CACHE_DIR=/var/cache/ixoss \
    IXOSS_BIND=0.0.0.0:8080 \
    IXOSS_SOURCE_ARCHIVE=/usr/share/doc/ixoss/source.tar.gz

USER ixoss:ixoss
EXPOSE 8080
ENTRYPOINT ["ixoss"]
