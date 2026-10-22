FROM golang:1.27-alpine3.23 AS build

# Require the Alpine bundle that distrusts the affected eMudhra roots.
RUN apk add --no-cache 'ca-certificates-bundle>=20261020-r0' \
    build-base pkgconf vips-dev vips-heif vips-jxl
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
ARG IXOSS_SOURCE_URL=""

RUN apk add --no-cache 'ca-certificates-bundle>=20261020-r0' \
    vips vips-heif vips-jxl font-wqy-zenhei \
    && addgroup -S -g 10001 ixoss \
    && adduser -S -D -H -u 10001 -G ixoss ixoss \
    && mkdir -p /srv/images /var/cache/ixoss \
    && chown ixoss:ixoss /srv/images /var/cache/ixoss

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
