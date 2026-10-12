FROM golang:1.26-alpine3.23 AS build

RUN apk add --no-cache build-base pkgconf vips-dev vips-heif vips-jxl
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
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

ENV IXOSS_ROOT=/srv/images \
    IXOSS_CACHE_DIR=/var/cache/ixoss \
    IXOSS_BIND=0.0.0.0:8080

USER ixoss:ixoss
EXPOSE 8080
ENTRYPOINT ["ixoss"]
